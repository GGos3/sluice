//go:build linux

package gateway

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// ProxyDialer handles forwarding intercepted HTTP requests to an upstream proxy.
// It manages connection establishment with routing bypass (SO_MARK) to avoid
// routing loops when the gateway is intercepting traffic.
type ProxyDialer struct {
	proxyAddr string
	proxyUser string
	proxyPass string
	fwmark    int
	timeout   time.Duration
}

// NewProxyDialer creates a new ProxyDialer for the given upstream proxy.
// The fwmark is used to mark outbound connections to bypass TUN routing.
func NewProxyDialer(proxyAddr string, proxyUser, proxyPass string, fwmark int) *ProxyDialer {
	return &ProxyDialer{
		proxyAddr: proxyAddr,
		proxyUser: proxyUser,
		proxyPass: proxyPass,
		fwmark:    fwmark,
		timeout:   30 * time.Second,
	}
}

// ForwardHTTP reads an HTTP request from the intercepted connection,
// rewrites it for the upstream proxy, and relays the response back.
// This is HTTP-only; HTTPS/CONNECT is handled separately.
//
// The dst parameter is the original destination address that was intercepted.
// The host parameter is the Host header value from the original request.
func (d *ProxyDialer) ForwardHTTP(ctx context.Context, conn net.Conn, dst netip.AddrPort, host string) error {
	req, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	defer req.Body.Close()

	outReq, err := d.rewriteRequest(req, dst, host)
	if err != nil {
		return fmt.Errorf("rewrite request: %w", err)
	}

	proxyConn, err := d.dialProxy(ctx)
	if err != nil {
		return fmt.Errorf("dial proxy: %w", err)
	}
	defer proxyConn.Close()

	if err := outReq.WriteProxy(proxyConn); err != nil {
		return fmt.Errorf("send request to proxy: %w", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(proxyConn), outReq)
	if err != nil {
		return fmt.Errorf("read proxy response: %w", err)
	}
	defer resp.Body.Close()

	return d.relayResponse(conn, resp)
}

func (d *ProxyDialer) ForwardHTTPS(ctx context.Context, conn net.Conn, dst netip.AddrPort, host string) error {
	serverName, replay, err := ExtractSNI(conn)
	if err != nil && len(replay) == 0 {
		serverName = ""
	}

	targetHost := chooseConnectHost(serverName, host, dst)
	targetAddr := net.JoinHostPort(targetHost, "443")

	proxyConn, err := d.dialProxy(ctx)
	if err != nil {
		return fmt.Errorf("dial proxy: %w", err)
	}
	defer proxyConn.Close()

	if err := d.sendConnect(proxyConn, targetAddr); err != nil {
		return err
	}

	if len(replay) > 0 {
		if _, err := proxyConn.Write(replay); err != nil {
			return fmt.Errorf("replay client hello: %w", err)
		}
	}

	transferBidirectional(proxyConn, conn)
	return nil
}

func (d *ProxyDialer) rewriteRequest(req *http.Request, dst netip.AddrPort, host string) (*http.Request, error) {
	targetHost := host
	if targetHost == "" {
		targetHost = dst.String()
	}

	var targetURL *url.URL
	if req.URL != nil {
		targetURL = cloneURL(req.URL)
		targetURL.Scheme = "http"
		if targetURL.Host == "" {
			targetURL.Host = targetHost
		}
	} else {
		targetURL = &url.URL{
			Scheme: "http",
			Host:   targetHost,
			Path:   "/",
		}
	}

	outReq := &http.Request{
		Method: req.Method,
		URL:    targetURL,
		Header: req.Header.Clone(),
		Body:   req.Body,
		Host:   targetURL.Host,
	}

	outReq.RequestURI = ""

	removeHopByHopHeaders(outReq.Header)

	if d.proxyUser != "" || d.proxyPass != "" {
		outReq.Header.Set("Proxy-Authorization", formatBasicAuth(d.proxyUser, d.proxyPass))
	}

	return outReq, nil
}

func (d *ProxyDialer) dialProxy(ctx context.Context) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout: d.timeout,
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				_ = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, d.fwmark)
			})
		},
	}

	return dialer.DialContext(ctx, "tcp", d.proxyAddr)
}

func (d *ProxyDialer) sendConnect(proxyConn net.Conn, targetAddr string) error {
	var builder strings.Builder
	builder.WriteString("CONNECT ")
	builder.WriteString(targetAddr)
	builder.WriteString(" HTTP/1.1\r\nHost: ")
	builder.WriteString(targetAddr)
	builder.WriteString("\r\n")
	if d.proxyUser != "" || d.proxyPass != "" {
		builder.WriteString("Proxy-Authorization: ")
		builder.WriteString(formatBasicAuth(d.proxyUser, d.proxyPass))
		builder.WriteString("\r\n")
	}
	builder.WriteString("\r\n")

	if _, err := io.WriteString(proxyConn, builder.String()); err != nil {
		return fmt.Errorf("send connect to proxy: %w", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(proxyConn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		return fmt.Errorf("read proxy connect response: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if len(body) > 0 {
			return fmt.Errorf("proxy connect failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}
		return fmt.Errorf("proxy connect failed: %s", resp.Status)
	}

	return nil
}

func (d *ProxyDialer) relayResponse(conn net.Conn, resp *http.Response) error {
	removeHopByHopHeaders(resp.Header)
	return resp.Write(conn)
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

func formatBasicAuth(username, password string) string {
	credentials := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", username, password)))
	return "Basic " + credentials
}

func chooseConnectHost(serverName, host string, dst netip.AddrPort) string {
	for _, candidate := range []string{serverName, host} {
		if normalized := normalizeConnectHost(candidate); normalized != "" {
			return normalized
		}
	}
	return dst.Addr().String()
}

func normalizeConnectHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		return parsedHost
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return strings.Trim(host, "[]")
	}
	return host
}

func transferBidirectional(dst, src net.Conn) (int64, int64) {
	type result struct {
		bytes     int64
		direction int
	}

	results := make(chan result, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	copyOne := func(direction int, writer net.Conn, reader net.Conn) {
		defer wg.Done()
		n, err := io.Copy(writer, reader)
		closeWrite(writer)
		if err != nil && !errors.Is(err, net.ErrClosed) {
			results <- result{bytes: n, direction: direction}
			return
		}
		results <- result{bytes: n, direction: direction}
	}

	go copyOne(0, dst, src)
	go copyOne(1, src, dst)

	go func() {
		wg.Wait()
		close(results)
	}()

	var srcToDst int64
	var dstToSrc int64
	for res := range results {
		if res.direction == 0 {
			srcToDst = res.bytes
		} else {
			dstToSrc = res.bytes
		}
	}

	return srcToDst, dstToSrc
}

func closeWrite(conn net.Conn) {
	type closeWriter interface {
		CloseWrite() error
	}
	if cw, ok := conn.(closeWriter); ok {
		_ = cw.CloseWrite()
		return
	}
	_ = conn.Close()
}
