package proxy

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ggos3/sluice/internal/acl"
	"github.com/ggos3/sluice/internal/dns"
	"github.com/ggos3/sluice/internal/logger"
)

type Handler struct {
	whitelist  *acl.Whitelist
	logger     *slog.Logger
	dnsHandler http.Handler
	transport  *http.Transport
	auth       *authConfig
}

type authConfig struct {
	credentials map[string]string
}

type Option func(*Handler)

func WithAuth(credentials map[string]string) Option {
	cloned := make(map[string]string, len(credentials))
	for username, password := range credentials {
		cloned[username] = password
	}

	return func(h *Handler) {
		h.auth = &authConfig{credentials: cloned}
	}
}

func NewHandler(whitelist *acl.Whitelist, logger *slog.Logger, args ...any) *Handler {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	h := &Handler{
		whitelist: whitelist,
		logger:    logger,
		transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}

	for _, arg := range args {
		switch value := arg.(type) {
		case nil:
			continue
		case http.Handler:
			if h.dnsHandler == nil {
				h.dnsHandler = value
			}
		case Option:
			if value != nil {
				value(h)
			}
		}
	}

	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.logger != nil {
		h.logger.Info("ServeHTTP entry",
			"method", r.Method,
			"url", r.URL.String(),
			"host", r.Host,
			"remote", r.RemoteAddr,
		)
	}

	if r.Method == http.MethodPost && r.URL != nil && !r.URL.IsAbs() && r.URL.Path == "/dns-query" {
		if h.dnsHandler == nil {
			http.NotFound(w, r)
			return
		}

		if !h.authorized(r) {
			w.Header().Set("Proxy-Authenticate", `Basic realm="proxy"`)
			http.Error(w, http.StatusText(http.StatusProxyAuthRequired), http.StatusProxyAuthRequired)
			return
		}

		payload, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		r.Body = io.NopCloser(bytes.NewReader(payload))

		domain, err := dns.QueryDomain(payload)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		if !h.isAllowed(domain) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}

		h.dnsHandler.ServeHTTP(w, r)
		return
	}

	if r.Method == http.MethodConnect {
		h.handleConnect(w, r)
		return
	}

	h.handleHTTP(w, r)
}

func (h *Handler) handleHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	domain := requestDomain(r)
	bytesIn := requestBytes(r)

	if h.logger != nil {
		h.logger.Info("handleHTTP",
			"domain", domain,
			"method", r.Method,
			"url", r.URL.String(),
			"host", r.Host,
		)
	}

	if !h.authorized(r) {
		w.Header().Set("Proxy-Authenticate", `Basic realm="proxy"`)
		h.writeErrorResponse(w, r, start, domain, bytesIn, http.StatusProxyAuthRequired, false, "proxy_auth_required")
		return
	}

	domainAllowed := h.isAllowed(domain)
	if h.logger != nil {
		h.logger.Info("isAllowed result",
			"domain", domain,
			"allowed", domainAllowed,
		)
	}

	if !domainAllowed {
		h.writeErrorResponse(w, r, start, domain, bytesIn, http.StatusForbidden, false, "domain_not_allowed")
		return
	}

	outReq := r.Clone(r.Context())
	if outReq.URL == nil {
		h.writeErrorResponse(w, r, start, domain, bytesIn, http.StatusBadRequest, false, "missing_url")
		return
	}

	outReq.RequestURI = ""
	outReq.URL = cloneURL(r.URL)
	if outReq.URL.Scheme == "" {
		outReq.URL.Scheme = "http"
	}
	if outReq.URL.Host == "" {
		outReq.URL.Host = r.Host
	}
	outReq.Host = outReq.URL.Host
	outReq.Header = r.Header.Clone()
	removeHopByHopHeaders(outReq.Header)

	resp, err := h.transport.RoundTrip(outReq)
	if err != nil {
		h.writeErrorResponse(w, r, start, domain, bytesIn, http.StatusBadGateway, false, "upstream_roundtrip_failed")
		return
	}
	defer resp.Body.Close()

	removeHopByHopHeaders(resp.Header)
	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	bytesOut, copyErr := io.Copy(w, resp.Body)
	allowed := copyErr == nil || errors.Is(copyErr, context.Canceled)
	if !allowed {
		if resp.StatusCode < 400 {
			resp.StatusCode = http.StatusBadGateway
		}
	}

	h.logAccess(r, logger.AccessLogEntry{
		SourceIP:   sourceIP(r.RemoteAddr),
		Method:     r.Method,
		Domain:     domain,
		Status:     resp.StatusCode,
		BytesIn:    bytesIn,
		BytesOut:   bytesOut,
		DurationMs: durationMillis(start),
		Allowed:    allowed,
		Reason:     accessReason(copyErr, "ok", "response_copy_failed"),
	})
}

func (h *Handler) authorized(r *http.Request) bool {
	if h.auth == nil {
		return true
	}

	value := strings.TrimSpace(r.Header.Get("Proxy-Authorization"))
	if value == "" {
		return false
	}

	scheme, encoded, ok := strings.Cut(value, " ")
	if !ok || !strings.EqualFold(strings.TrimSpace(scheme), "Basic") {
		return false
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return false
	}

	username, password, ok := strings.Cut(string(decoded), ":")
	if !ok {
		return false
	}

	matched := false
	for expectedUsername, expectedPassword := range h.auth.credentials {
		usernameMatch := subtle.ConstantTimeCompare([]byte(username), []byte(expectedUsername)) == 1
		passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(expectedPassword)) == 1
		if usernameMatch && passwordMatch {
			matched = true
		}
	}

	return matched
}

func (h *Handler) isAllowed(host string) bool {
	if h.whitelist == nil {
		return false
	}
	return h.whitelist.IsAllowed(host)
}

func (h *Handler) writeErrorResponse(w http.ResponseWriter, r *http.Request, start time.Time, domain string, bytesIn int64, status int, allowed bool, reason string) {
	http.Error(w, http.StatusText(status), status)
	h.logAccess(r, logger.AccessLogEntry{
		SourceIP:   sourceIP(r.RemoteAddr),
		Method:     r.Method,
		Domain:     domain,
		Status:     status,
		BytesIn:    bytesIn,
		BytesOut:   0,
		DurationMs: durationMillis(start),
		Allowed:    allowed,
		Reason:     reason,
	})
}

func (h *Handler) logAccess(r *http.Request, entry logger.AccessLogEntry) {
	logger.LogAccess(h.logger, entry)
}

func requestDomain(r *http.Request) string {
	if r == nil {
		return ""
	}
	if r.URL != nil && r.URL.Host != "" {
		return r.URL.Host
	}
	return r.Host
}

func requestBytes(r *http.Request) int64 {
	if r == nil || r.ContentLength < 0 {
		return 0
	}
	return r.ContentLength
}

func cloneURL(src *url.URL) *url.URL {
	if src == nil {
		return nil
	}
	cloned := *src
	return &cloned
}

func removeHopByHopHeaders(header http.Header) {
	if header == nil {
		return
	}

	for _, value := range header.Values("Connection") {
		for _, field := range strings.Split(value, ",") {
			if name := strings.TrimSpace(field); name != "" {
				header.Del(name)
			}
		}
	}

	for _, name := range []string{
		"Connection",
		"Proxy-Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"TE",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	} {
		header.Del(name)
	}
}

func copyHeaders(dst, src http.Header) {
	for name, values := range src {
		for _, value := range values {
			dst.Add(name, value)
		}
	}
}

func sourceIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func durationMillis(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}

func accessReason(err error, okReason string, failReason string) string {
	if err != nil && !errors.Is(err, context.Canceled) {
		return failReason
	}
	return okReason
}

func formatBasicAuth(username, password string) string {
	credentials := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", username, password)))
	return "Basic " + credentials
}
