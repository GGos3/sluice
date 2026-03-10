package dns

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strings"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	contentTypeDNSMessage = "application/dns-message"
	defaultTTL            = 60
	maxDNSMessageSize     = 65535
)

type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type Handler struct {
	logger   *slog.Logger
	resolver Resolver
}

type Option func(*Handler)

func WithResolver(resolver Resolver) Option {
	return func(h *Handler) {
		if resolver != nil {
			h.resolver = resolver
		}
	}
}

func NewHandler(logger *slog.Logger, opts ...Option) *Handler {
	h := &Handler{
		logger:   logger,
		resolver: net.DefaultResolver,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}

	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL == nil || r.URL.Path != "/dns-query" {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if !isDNSMessageContentType(r.Header.Get("Content-Type")) {
		http.Error(w, http.StatusText(http.StatusUnsupportedMediaType), http.StatusUnsupportedMediaType)
		return
	}

	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxDNSMessageSize))
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	header, question, err := parseQuery(payload)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	domain := canonicalDomain(question.Name)
	if domain == "" {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	rcode, addrs := h.resolve(r.Context(), domain, question.Type)
	h.logQuery(domain, question.Type, rcode)

	response, err := buildResponse(header, question, rcode, addrs)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentTypeDNSMessage)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(response)
}

func isDNSMessageContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && mediaType == contentTypeDNSMessage
}

func parseQuery(payload []byte) (dnsmessage.Header, dnsmessage.Question, error) {
	var parser dnsmessage.Parser
	header, err := parser.Start(payload)
	if err != nil {
		return dnsmessage.Header{}, dnsmessage.Question{}, err
	}
	if header.Response {
		return dnsmessage.Header{}, dnsmessage.Question{}, errors.New("unexpected response message")
	}

	question, err := parser.Question()
	if err != nil {
		return dnsmessage.Header{}, dnsmessage.Question{}, err
	}

	_, err = parser.Question()
	if err == nil {
		return dnsmessage.Header{}, dnsmessage.Question{}, errors.New("multiple questions not supported")
	}
	if !errors.Is(err, dnsmessage.ErrSectionDone) {
		return dnsmessage.Header{}, dnsmessage.Question{}, err
	}

	return header, question, nil
}

func canonicalDomain(name dnsmessage.Name) string {
	return strings.TrimSuffix(name.String(), ".")
}

func QueryDomain(payload []byte) (string, error) {
	_, question, err := parseQuery(payload)
	if err != nil {
		return "", err
	}

	domain := canonicalDomain(question.Name)
	if domain == "" {
		return "", errors.New("empty query domain")
	}

	return domain, nil
}

func (h *Handler) resolve(ctx context.Context, domain string, typ dnsmessage.Type) (dnsmessage.RCode, []net.IPAddr) {
	if typ != dnsmessage.TypeA && typ != dnsmessage.TypeAAAA {
		return dnsmessage.RCodeNotImplemented, nil
	}

	addrs, err := h.resolver.LookupIPAddr(ctx, domain)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return dnsmessage.RCodeNameError, nil
		}
		return dnsmessage.RCodeServerFailure, nil
	}

	filtered := make([]net.IPAddr, 0, len(addrs))
	for _, addr := range addrs {
		switch typ {
		case dnsmessage.TypeA:
			if v4 := addr.IP.To4(); v4 != nil {
				filtered = append(filtered, net.IPAddr{IP: append(net.IP(nil), v4...)})
			}
		case dnsmessage.TypeAAAA:
			if v16 := addr.IP.To16(); v16 != nil && addr.IP.To4() == nil {
				filtered = append(filtered, net.IPAddr{IP: append(net.IP(nil), v16...)})
			}
		}
	}

	return dnsmessage.RCodeSuccess, filtered
}

func buildResponse(header dnsmessage.Header, question dnsmessage.Question, rcode dnsmessage.RCode, addrs []net.IPAddr) ([]byte, error) {
	builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{
		ID:                 header.ID,
		Response:           true,
		RecursionDesired:   header.RecursionDesired,
		RecursionAvailable: true,
		RCode:              rcode,
	})
	builder.EnableCompression()

	if err := builder.StartQuestions(); err != nil {
		return nil, err
	}
	if err := builder.Question(question); err != nil {
		return nil, err
	}
	if err := builder.StartAnswers(); err != nil {
		return nil, err
	}

	resourceHeader := dnsmessage.ResourceHeader{
		Name:  question.Name,
		Class: question.Class,
		TTL:   defaultTTL,
	}

	for _, addr := range addrs {
		switch question.Type {
		case dnsmessage.TypeA:
			v4 := addr.IP.To4()
			if v4 == nil {
				continue
			}
			var resource dnsmessage.AResource
			copy(resource.A[:], v4)
			resourceHeader.Type = dnsmessage.TypeA
			if err := builder.AResource(resourceHeader, resource); err != nil {
				return nil, err
			}
		case dnsmessage.TypeAAAA:
			v16 := addr.IP.To16()
			if v16 == nil || addr.IP.To4() != nil {
				continue
			}
			var resource dnsmessage.AAAAResource
			copy(resource.AAAA[:], v16)
			resourceHeader.Type = dnsmessage.TypeAAAA
			if err := builder.AAAAResource(resourceHeader, resource); err != nil {
				return nil, err
			}
		}
	}

	return builder.Finish()
}

func (h *Handler) logQuery(domain string, typ dnsmessage.Type, rcode dnsmessage.RCode) {
	if h.logger == nil {
		return
	}

	h.logger.Info("dns query",
		slog.String("domain", domain),
		slog.String("type", typeName(typ)),
		slog.String("rcode", rcodeName(rcode)),
	)
}

func typeName(typ dnsmessage.Type) string {
	switch typ {
	case dnsmessage.TypeA:
		return "A"
	case dnsmessage.TypeAAAA:
		return "AAAA"
	default:
		return "UNKNOWN"
	}
}

func rcodeName(rcode dnsmessage.RCode) string {
	switch rcode {
	case dnsmessage.RCodeSuccess:
		return "NOERROR"
	case dnsmessage.RCodeNameError:
		return "NXDOMAIN"
	case dnsmessage.RCodeServerFailure:
		return "SERVFAIL"
	case dnsmessage.RCodeNotImplemented:
		return "NOTIMP"
	default:
		return "UNKNOWN"
	}
}
