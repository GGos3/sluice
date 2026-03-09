package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ggos3/sluice/internal/acl"
)

func TestHandleHTTPAllowedRequest(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "ok")
		_, _ = io.WriteString(w, "forwarded")
	}))
	t.Cleanup(backend.Close)

	handler := NewHandler(acl.New(true, []string{"127.0.0.1"}), nil)
	proxyServer := httptest.NewServer(handler)
	t.Cleanup(proxyServer.Close)

	resp := doProxyRequest(t, proxyServer.URL, backend.URL, "", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if got := string(body); got != "forwarded" {
		t.Fatalf("body = %q, want %q", got, "forwarded")
	}

	if got := resp.Header.Get("X-Backend"); got != "ok" {
		t.Fatalf("X-Backend = %q, want %q", got, "ok")
	}
}

func TestHandleHTTPDeniedRequest(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("backend should not be reached")
	}))
	t.Cleanup(backend.Close)

	handler := NewHandler(acl.New(true, []string{"example.com"}), nil)
	proxyServer := httptest.NewServer(handler)
	t.Cleanup(proxyServer.Close)

	resp := doProxyRequest(t, proxyServer.URL, backend.URL, "", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestHandleHTTPStripsHopByHopHeaders(t *testing.T) {
	requestHeaders := make(chan http.Header, 1)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestHeaders <- r.Header.Clone()
		w.Header().Set("Connection", "X-Remove-Me")
		w.Header().Set("Proxy-Connection", "keep-alive")
		w.Header().Set("Keep-Alive", "timeout=5")
		w.Header().Set("Transfer-Encoding", "chunked")
		w.Header().Set("X-Remove-Me", "gone")
		w.Header().Set("X-Stays", "present")
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(backend.Close)

	handler := NewHandler(acl.New(true, []string{"127.0.0.1"}), nil)
	proxyServer := httptest.NewServer(handler)
	t.Cleanup(proxyServer.Close)

	headers := http.Header{}
	headers.Set("Connection", "Keep-Alive, X-Custom-Hop")
	headers.Set("Proxy-Connection", "keep-alive")
	headers.Set("Keep-Alive", "timeout=5")
	headers.Set("Proxy-Authorization", formatBasicAuth("user", "pass"))
	headers.Set("X-Custom-Hop", "remove")
	headers.Set("X-End-To-End", "keep")

	resp := doProxyRequest(t, proxyServer.URL, backend.URL, "", headers)
	defer resp.Body.Close()

	select {
	case got := <-requestHeaders:
		for _, name := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authorization", "X-Custom-Hop"} {
			if got.Get(name) != "" {
				t.Fatalf("request header %s was not stripped", name)
			}
		}
		if got.Get("X-End-To-End") != "keep" {
			t.Fatalf("X-End-To-End = %q, want keep", got.Get("X-End-To-End"))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for backend request")
	}

	if resp.Header.Get("Connection") != "" || resp.Header.Get("Proxy-Connection") != "" || resp.Header.Get("Keep-Alive") != "" || resp.Header.Get("X-Remove-Me") != "" {
		t.Fatalf("hop-by-hop response headers were not stripped: %#v", resp.Header)
	}
	if got := resp.Header.Get("X-Stays"); got != "present" {
		t.Fatalf("X-Stays = %q, want present", got)
	}
}

func TestHandleHTTPAuthRequiredMissing(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("backend should not be reached")
	}))
	t.Cleanup(backend.Close)

	handler := NewHandler(acl.New(true, []string{"127.0.0.1"}), nil, WithAuth(map[string]string{"user": "pass"}))
	proxyServer := httptest.NewServer(handler)
	t.Cleanup(proxyServer.Close)

	resp := doProxyRequest(t, proxyServer.URL, backend.URL, "", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusProxyAuthRequired {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusProxyAuthRequired)
	}
	if got := resp.Header.Get("Proxy-Authenticate"); !strings.Contains(got, "Basic") {
		t.Fatalf("Proxy-Authenticate = %q, want Basic challenge", got)
	}
}

func TestHandleHTTPAuthValidCredentials(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(backend.Close)

	handler := NewHandler(acl.New(true, []string{"127.0.0.1"}), nil, WithAuth(map[string]string{"user": "pass"}))
	proxyServer := httptest.NewServer(handler)
	t.Cleanup(proxyServer.Close)

	headers := http.Header{}
	headers.Set("Proxy-Authorization", formatBasicAuth("user", "pass"))
	resp := doProxyRequest(t, proxyServer.URL, backend.URL, "", headers)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHandleHTTPAuthInvalidCredentials(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("backend should not be reached")
	}))
	t.Cleanup(backend.Close)

	handler := NewHandler(acl.New(true, []string{"127.0.0.1"}), nil, WithAuth(map[string]string{"user": "pass"}))
	proxyServer := httptest.NewServer(handler)
	t.Cleanup(proxyServer.Close)

	headers := http.Header{}
	headers.Set("Proxy-Authorization", formatBasicAuth("user", "wrong"))
	resp := doProxyRequest(t, proxyServer.URL, backend.URL, "", headers)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusProxyAuthRequired {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusProxyAuthRequired)
	}
}

func TestHandleHTTPAccessLogging(t *testing.T) {
	var logs safeBuffer
	accessLogger := slog.New(slog.NewJSONHandler(&logs, nil))

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "logged")
	}))
	t.Cleanup(backend.Close)

	handler := NewHandler(acl.New(true, []string{"127.0.0.1"}), accessLogger)
	proxyServer := httptest.NewServer(handler)
	t.Cleanup(proxyServer.Close)

	resp := doProxyRequest(t, proxyServer.URL, backend.URL, "", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	entry := parseAccessLogLine(t, logs.String())
	proxy := entry["proxy"].(map[string]any)
	if proxy["method"] != http.MethodGet {
		t.Fatalf("method = %v, want %s", proxy["method"], http.MethodGet)
	}
	if proxy["domain"] != hostPort(t, backend.URL) {
		t.Fatalf("domain = %v, want %s", proxy["domain"], hostPort(t, backend.URL))
	}
	if int(proxy["status"].(float64)) != http.StatusOK {
		t.Fatalf("status = %v, want %d", proxy["status"], http.StatusOK)
	}
	if proxy["allowed"] != true {
		t.Fatalf("allowed = %v, want true", proxy["allowed"])
	}
	if proxy["reason"] != "ok" {
		t.Fatalf("reason = %v, want ok", proxy["reason"])
	}
}

func doProxyRequest(t *testing.T, proxyURL, targetURL, method string, headers http.Header) *http.Response {
	t.Helper()
	if method == "" {
		method = http.MethodGet
	}

	proxyParsed, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}

	transport := &http.Transport{Proxy: http.ProxyURL(proxyParsed)}
	client := &http.Client{Transport: transport}
	t.Cleanup(transport.CloseIdleConnections)

	req, err := http.NewRequestWithContext(context.Background(), method, targetURL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client do: %v", err)
	}
	return resp
}

func parseAccessLogLine(t *testing.T, content string) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("expected access log output")
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
		t.Fatalf("unmarshal log entry: %v", err)
	}
	return entry
}

func hostPort(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return parsed.Host
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
