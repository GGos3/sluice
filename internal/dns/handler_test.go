package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func TestHandlerReturnsARecord(t *testing.T) {
	handler := NewHandler(nil, WithResolver(stubResolver{
		lookupIPAddr: func(ctx context.Context, host string) ([]net.IPAddr, error) {
			if host != "example.com" {
				t.Fatalf("host = %q, want %q", host, "example.com")
			}
			return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}, {IP: net.ParseIP("2001:db8::10")}}, nil
		},
	}))

	resp := doRequest(t, handler, buildQuery(t, "example.com.", dnsmessage.TypeA))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != contentTypeDNSMessage {
		t.Fatalf("Content-Type = %q, want %q", got, contentTypeDNSMessage)
	}

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	header, questions, answers := parseResponse(t, payload)
	if !header.Response {
		t.Fatal("expected response header")
	}
	if header.RCode != dnsmessage.RCodeSuccess {
		t.Fatalf("rcode = %v, want %v", header.RCode, dnsmessage.RCodeSuccess)
	}
	if len(questions) != 1 {
		t.Fatalf("questions = %d, want 1", len(questions))
	}
	if len(answers) != 1 {
		t.Fatalf("answers = %d, want 1", len(answers))
	}
	if got := answers[0].Header.Type; got != dnsmessage.TypeA {
		t.Fatalf("answer type = %v, want %v", got, dnsmessage.TypeA)
	}
	if got := resourceIP(t, answers[0].Body); got != "203.0.113.10" {
		t.Fatalf("answer ip = %q, want %q", got, "203.0.113.10")
	}
}

func TestHandlerReturnsAAAARecord(t *testing.T) {
	handler := NewHandler(nil, WithResolver(stubResolver{
		lookupIPAddr: func(ctx context.Context, host string) ([]net.IPAddr, error) {
			if host != "example.com" {
				t.Fatalf("host = %q, want %q", host, "example.com")
			}
			return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}, {IP: net.ParseIP("2001:db8::10")}}, nil
		},
	}))

	resp := doRequest(t, handler, buildQuery(t, "example.com.", dnsmessage.TypeAAAA))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	header, _, answers := parseResponse(t, payload)
	if header.RCode != dnsmessage.RCodeSuccess {
		t.Fatalf("rcode = %v, want %v", header.RCode, dnsmessage.RCodeSuccess)
	}
	if len(answers) != 1 {
		t.Fatalf("answers = %d, want 1", len(answers))
	}
	if got := answers[0].Header.Type; got != dnsmessage.TypeAAAA {
		t.Fatalf("answer type = %v, want %v", got, dnsmessage.TypeAAAA)
	}
	if got := resourceIP(t, answers[0].Body); got != "2001:db8::10" {
		t.Fatalf("answer ip = %q, want %q", got, "2001:db8::10")
	}
}

func TestHandlerReturnsNXDOMAIN(t *testing.T) {
	handler := NewHandler(nil, WithResolver(stubResolver{
		lookupIPAddr: func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		},
	}))

	resp := doRequest(t, handler, buildQuery(t, "missing.example.", dnsmessage.TypeA))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	header, _, answers := parseResponse(t, payload)
	if header.RCode != dnsmessage.RCodeNameError {
		t.Fatalf("rcode = %v, want %v", header.RCode, dnsmessage.RCodeNameError)
	}
	if len(answers) != 0 {
		t.Fatalf("answers = %d, want 0", len(answers))
	}
}

func TestHandlerReturnsBadRequestForMalformedBody(t *testing.T) {
	handler := NewHandler(nil, WithResolver(stubResolver{
		lookupIPAddr: func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return nil, errors.New("resolver should not be called")
		},
	}))

	req := httptest.NewRequest(http.MethodPost, "/dns-query", bytes.NewReader([]byte("not-dns")))
	req.Header.Set("Content-Type", contentTypeDNSMessage)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusBadRequest)
	}
}

func TestHandlerLogsResolvedDomain(t *testing.T) {
	var logs safeBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := NewHandler(logger, WithResolver(stubResolver{
		lookupIPAddr: func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
		},
	}))

	resp := doRequest(t, handler, buildQuery(t, "example.com.", dnsmessage.TypeA))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	entry := parseLogLine(t, logs.String())
	if got := entry["msg"]; got != "dns query" {
		t.Fatalf("msg = %v, want %q", got, "dns query")
	}
	if got := entry["domain"]; got != "example.com" {
		t.Fatalf("domain = %v, want %q", got, "example.com")
	}
	if got := entry["type"]; got != "A" {
		t.Fatalf("type = %v, want %q", got, "A")
	}
	if got := entry["rcode"]; got != "NOERROR" {
		t.Fatalf("rcode = %v, want %q", got, "NOERROR")
	}
}

func TestQueryDomain(t *testing.T) {
	domain, err := QueryDomain(buildQuery(t, "example.com.", dnsmessage.TypeA))
	if err != nil {
		t.Fatalf("QueryDomain returned error: %v", err)
	}
	if domain != "example.com" {
		t.Fatalf("domain = %q, want %q", domain, "example.com")
	}
}

func TestQueryDomainMalformed(t *testing.T) {
	if _, err := QueryDomain([]byte("not-dns")); err == nil {
		t.Fatal("expected QueryDomain to fail for malformed payload")
	}
}

func doRequest(t *testing.T, handler http.Handler, payload []byte) *http.Response {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL+"/dns-query", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", contentTypeDNSMessage)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func buildQuery(t *testing.T, domain string, typ dnsmessage.Type) []byte {
	t.Helper()

	name, err := dnsmessage.NewName(domain)
	if err != nil {
		t.Fatalf("new name: %v", err)
	}

	builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: 42, RecursionDesired: true})
	builder.EnableCompression()
	if err := builder.StartQuestions(); err != nil {
		t.Fatalf("start questions: %v", err)
	}
	if err := builder.Question(dnsmessage.Question{Name: name, Type: typ, Class: dnsmessage.ClassINET}); err != nil {
		t.Fatalf("question: %v", err)
	}

	payload, err := builder.Finish()
	if err != nil {
		t.Fatalf("finish query: %v", err)
	}
	return payload
}

func parseResponse(t *testing.T, payload []byte) (dnsmessage.Header, []dnsmessage.Question, []dnsmessage.Resource) {
	t.Helper()

	var parser dnsmessage.Parser
	header, err := parser.Start(payload)
	if err != nil {
		t.Fatalf("start parser: %v", err)
	}

	questions, err := parser.AllQuestions()
	if err != nil {
		t.Fatalf("questions: %v", err)
	}
	answers, err := parser.AllAnswers()
	if err != nil {
		t.Fatalf("answers: %v", err)
	}

	return header, questions, answers
}

func resourceIP(t *testing.T, body dnsmessage.ResourceBody) string {
	t.Helper()

	switch value := body.(type) {
	case *dnsmessage.AResource:
		return net.IP(value.A[:]).String()
	case *dnsmessage.AAAAResource:
		return net.IP(value.AAAA[:]).String()
	default:
		t.Fatalf("unexpected resource body type %T", body)
		return ""
	}
}

func parseLogLine(t *testing.T, content string) map[string]any {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("expected log output")
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
		t.Fatalf("unmarshal log entry: %v", err)
	}
	return entry
}

type stubResolver struct {
	lookupIPAddr func(context.Context, string) ([]net.IPAddr, error)
}

func (r stubResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return r.lookupIPAddr(ctx, host)
}

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
