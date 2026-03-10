//go:build linux

package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNewProxyDialer(t *testing.T) {
	t.Parallel()

	d := NewProxyDialer("192.168.1.100:8080", "user", "pass", 0x1)

	if d.proxyAddr != "192.168.1.100:8080" {
		t.Fatalf("proxyAddr = %q, want %q", d.proxyAddr, "192.168.1.100:8080")
	}
	if d.proxyUser != "user" {
		t.Fatalf("proxyUser = %q, want %q", d.proxyUser, "user")
	}
	if d.proxyPass != "pass" {
		t.Fatalf("proxyPass = %q, want %q", d.proxyPass, "pass")
	}
	if d.fwmark != 0x1 {
		t.Fatalf("fwmark = %d, want %d", d.fwmark, 0x1)
	}
	if d.timeout != 30*time.Second {
		t.Fatalf("timeout = %v, want %v", d.timeout, 30*time.Second)
	}
}

func TestRewriteRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		method    string
		path      string
		host      string
		dst       netip.AddrPort
		proxyUser string
		proxyPass string
		wantURL   string
		wantHost  string
		wantAuth  bool
	}{
		{
			name:     "relative path with host",
			method:   "GET",
			path:     "/api/data",
			host:     "example.com",
			dst:      netip.MustParseAddrPort("93.184.216.34:80"),
			wantURL:  "http://example.com/api/data",
			wantHost: "example.com",
			wantAuth: false,
		},
		{
			name:      "with auth credentials",
			method:    "POST",
			path:      "/submit",
			host:      "api.example.com",
			dst:       netip.MustParseAddrPort("10.0.0.1:80"),
			proxyUser: "user",
			proxyPass: "pass",
			wantURL:   "http://api.example.com/submit",
			wantHost:  "api.example.com",
			wantAuth:  true,
		},
		{
			name:     "empty host uses dst",
			method:   "GET",
			path:     "/",
			host:     "",
			dst:      netip.MustParseAddrPort("192.168.1.1:8080"),
			wantURL:  "http://192.168.1.1:8080/",
			wantHost: "192.168.1.1:8080",
			wantAuth: false,
		},
		{
			name:     "query params preserved",
			method:   "GET",
			path:     "/search?q=golang&page=1",
			host:     "google.com",
			dst:      netip.MustParseAddrPort("1.2.3.4:80"),
			wantURL:  "http://google.com/search?q=golang&page=1",
			wantHost: "google.com",
			wantAuth: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := NewProxyDialer("proxy:8080", tt.proxyUser, tt.proxyPass, 0x1)

			req := &http.Request{
				Method: tt.method,
				URL:    mustParseURL(tt.path),
				Header: http.Header{},
				Body:   io.NopCloser(strings.NewReader("")),
			}

			outReq, err := d.rewriteRequest(req, tt.dst, tt.host)
			if err != nil {
				t.Fatalf("rewriteRequest() error = %v", err)
			}

			if outReq.URL.String() != tt.wantURL {
				t.Fatalf("URL = %q, want %q", outReq.URL.String(), tt.wantURL)
			}

			if outReq.Host != tt.wantHost {
				t.Fatalf("Host = %q, want %q", outReq.Host, tt.wantHost)
			}

			auth := outReq.Header.Get("Proxy-Authorization")
			if tt.wantAuth && auth == "" {
				t.Fatal("expected Proxy-Authorization header, got none")
			}
			if !tt.wantAuth && auth != "" {
				t.Fatalf("unexpected Proxy-Authorization header: %q", auth)
			}

			if outReq.RequestURI != "" {
				t.Fatalf("RequestURI = %q, want empty", outReq.RequestURI)
			}
		})
	}
}

func TestRewriteRequestWritesAbsoluteForm(t *testing.T) {
	t.Parallel()

	d := NewProxyDialer("proxy:8080", "testuser", "testpass", 0x1)

	req := &http.Request{
		Method: "GET",
		URL:    mustParseURL("/resource?id=123"),
		Header: http.Header{"User-Agent": []string{"test-agent"}},
		Body:   io.NopCloser(strings.NewReader("")),
	}

	outReq, err := d.rewriteRequest(req, netip.MustParseAddrPort("10.0.0.1:80"), "target.example.com:8080")
	if err != nil {
		t.Fatalf("rewriteRequest() error = %v", err)
	}

	var buf bytes.Buffer
	if err := outReq.WriteProxy(&buf); err != nil {
		t.Fatalf("outReq.WriteProxy() error = %v", err)
	}

	rawReq := buf.String()

	if !strings.Contains(rawReq, "GET http://target.example.com:8080/resource?id=123") {
		t.Fatalf("request line = %q, want absolute-form URL", rawReq[:100])
	}

	if !strings.Contains(rawReq, "Proxy-Authorization: Basic ") {
		t.Fatalf("request missing Proxy-Authorization header: %q", rawReq)
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(
		strings.TrimSpace(strings.Split(rawReq, "Proxy-Authorization: Basic ")[1]),
		"\r"))
	if err != nil {
		t.Fatalf("failed to decode auth: %v", err)
	}
	if string(decoded) != "testuser:testpass" {
		t.Fatalf("decoded auth = %q, want testuser:testpass", decoded)
	}
}

func TestRewriteRequestStripsHopByHop(t *testing.T) {
	t.Parallel()

	d := NewProxyDialer("proxy:8080", "", "", 0x1)

	req := &http.Request{
		Method: "GET",
		URL:    mustParseURL("/test"),
		Header: http.Header{
			"Connection":        []string{"keep-alive"},
			"Keep-Alive":        []string{"timeout=5"},
			"Transfer-Encoding": []string{"chunked"},
			"X-Custom":          []string{"value"},
			"User-Agent":        []string{"test"},
		},
		Body: io.NopCloser(strings.NewReader("")),
	}

	outReq, err := d.rewriteRequest(req, netip.MustParseAddrPort("10.0.0.1:80"), "example.com")
	if err != nil {
		t.Fatalf("rewriteRequest() error = %v", err)
	}

	if outReq.Header.Get("Connection") != "" {
		t.Fatal("Connection header should be stripped")
	}
	if outReq.Header.Get("Keep-Alive") != "" {
		t.Fatal("Keep-Alive header should be stripped")
	}
	if outReq.Header.Get("Transfer-Encoding") != "" {
		t.Fatal("Transfer-Encoding header should be stripped")
	}
	if outReq.Header.Get("X-Custom") != "value" {
		t.Fatal("X-Custom header should be preserved")
	}
	if outReq.Header.Get("User-Agent") != "test" {
		t.Fatal("User-Agent header should be preserved")
	}
}

func TestRemoveHopByHopHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    http.Header
		wantGone []string
		wantKeep map[string]string
	}{
		{
			name: "removes standard hop-by-hop headers",
			input: http.Header{
				"Connection":        []string{"keep-alive"},
				"Keep-Alive":        []string{"timeout=5"},
				"Transfer-Encoding": []string{"chunked"},
				"X-Custom":          []string{"value"},
			},
			wantGone: []string{"Connection", "Keep-Alive", "Transfer-Encoding"},
			wantKeep: map[string]string{"X-Custom": "value"},
		},
		{
			name: "removes headers listed in Connection",
			input: http.Header{
				"Connection":    []string{"X-Remove-Me, X-Also-Remove"},
				"X-Remove-Me":   []string{"gone"},
				"X-Also-Remove": []string{"gone"},
				"X-Keep":        []string{"kept"},
			},
			wantGone: []string{"Connection", "X-Remove-Me", "X-Also-Remove"},
			wantKeep: map[string]string{"X-Keep": "kept"},
		},
		{
			name:     "handles nil header",
			input:    nil,
			wantGone: nil,
			wantKeep: nil,
		},
		{
			name: "removes proxy auth headers",
			input: http.Header{
				"Proxy-Authenticate":  []string{"Basic realm=\"test\""},
				"Proxy-Authorization": []string{"Basic dGVzdA=="},
				"Content-Type":        []string{"application/json"},
			},
			wantGone: []string{"Proxy-Authenticate", "Proxy-Authorization"},
			wantKeep: map[string]string{"Content-Type": "application/json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var h http.Header
			if tt.input != nil {
				h = tt.input.Clone()
			}
			removeHopByHopHeaders(h)

			for _, name := range tt.wantGone {
				if h != nil && h.Get(name) != "" {
					t.Fatalf("header %q should be removed, got %q", name, h.Get(name))
				}
			}

			for name, value := range tt.wantKeep {
				if h == nil || h.Get(name) != value {
					t.Fatalf("header %q = %q, want %q", name, h.Get(name), value)
				}
			}
		})
	}
}

func TestFormatBasicAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		username string
		password string
		want     string
	}{
		{
			name:     "standard credentials",
			username: "user",
			password: "pass",
			want:     "Basic dXNlcjpwYXNz",
		},
		{
			name:     "empty password",
			username: "user",
			password: "",
			want:     "Basic dXNlcjo=",
		},
		{
			name:     "complex password",
			username: "admin",
			password: "p@ssw0rd!",
			want:     "Basic YWRtaW46cEBzc3cwcmQh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := formatBasicAuth(tt.username, tt.password)
			if got != tt.want {
				t.Fatalf("formatBasicAuth() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRelayResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		headers    http.Header
		body       string
	}{
		{
			name:       "200 OK with body",
			statusCode: http.StatusOK,
			headers:    http.Header{"Content-Type": []string{"text/plain"}},
			body:       "Hello, World!",
		},
		{
			name:       "201 Created with JSON",
			statusCode: http.StatusCreated,
			headers:    http.Header{"Content-Type": []string{"application/json"}},
			body:       `{"status":"ok"}`,
		},
		{
			name:       "204 No Content",
			statusCode: http.StatusNoContent,
			headers:    http.Header{},
			body:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp := &http.Response{
				StatusCode: tt.statusCode,
				Header:     tt.headers.Clone(),
				Body:       io.NopCloser(strings.NewReader(tt.body)),
			}

			conn := newMockConn()
			d := &ProxyDialer{}
			err := d.relayResponse(conn, resp)
			if err != nil {
				t.Fatalf("relayResponse() error = %v", err)
			}

			result := conn.String()

			statusText := http.StatusText(tt.statusCode)
			if !strings.Contains(result, fmt.Sprintf("%d %s", tt.statusCode, statusText)) {
				t.Fatalf("response missing status line: %q", result[:100])
			}

			for key, values := range tt.headers {
				for _, val := range values {
					if !strings.Contains(result, fmt.Sprintf("%s: %s", key, val)) {
						t.Fatalf("response missing header %s: %s", key, val)
					}
				}
			}

			if tt.body != "" && !strings.Contains(result, tt.body) {
				t.Fatalf("response missing body: %q", result)
			}
		})
	}
}

func TestRelayResponseStripsHopByHop(t *testing.T) {
	t.Parallel()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":      []string{"text/plain"},
			"Connection":        []string{"keep-alive"},
			"Keep-Alive":        []string{"timeout=5"},
			"Transfer-Encoding": []string{"chunked"},
		},
		Body: io.NopCloser(strings.NewReader("test")),
	}

	conn := newMockConn()
	d := &ProxyDialer{}
	err := d.relayResponse(conn, resp)
	if err != nil {
		t.Fatalf("relayResponse() error = %v", err)
	}

	result := conn.String()

	if strings.Contains(result, "Connection:") {
		t.Fatal("Connection header should be stripped from response")
	}
	if strings.Contains(result, "Keep-Alive:") {
		t.Fatal("Keep-Alive header should be stripped from response")
	}
	if strings.Contains(result, "Transfer-Encoding:") {
		t.Fatal("Transfer-Encoding header should be stripped from response")
	}
	if !strings.Contains(result, "Content-Type: text/plain") {
		t.Fatal("Content-Type header should be preserved in response")
	}
}

func TestForwardHTTPIntegration(t *testing.T) {
	t.Parallel()

	var receivedMethod string
	var receivedURL string
	var receivedAuth string
	var receivedHeaders = make(map[string]string)

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedURL = r.URL.String()
		receivedAuth = r.Header.Get("Proxy-Authorization")
		for key, values := range r.Header {
			if len(values) > 0 {
				receivedHeaders[key] = values[0]
			}
		}
		w.Header().Set("X-Test-Response", "ok")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response body"))
	}))
	defer proxy.Close()

	proxyAddr := strings.TrimPrefix(proxy.URL, "http://")

	d := NewProxyDialer(proxyAddr, "testuser", "testpass", 0x1)

	clientListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer clientListener.Close()

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := clientListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = d.ForwardHTTP(ctx, conn, netip.MustParseAddrPort("10.0.0.1:80"), "example.com")
		if err != nil {
			t.Errorf("ForwardHTTP() error = %v", err)
		}
	}()

	dialConn, err := net.Dial("tcp", clientListener.Addr().String())
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}

	req := "GET /test/path HTTP/1.1\r\n" +
		"Host: example.com\r\n" +
		"Content-Length: 0\r\n" +
		"\r\n"
	dialConn.Write([]byte(req))

	resp, err := http.ReadResponse(bufio.NewReader(dialConn), nil)
	dialConn.Close()

	<-serverDone

	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if resp.Header.Get("X-Test-Response") != "ok" {
		t.Fatal("missing X-Test-Response header")
	}

	if receivedMethod != "GET" {
		t.Fatalf("received method = %q, want GET", receivedMethod)
	}

	if receivedURL != "http://example.com/test/path" {
		t.Fatalf("received URL = %q, want http://example.com/test/path", receivedURL)
	}

	if receivedAuth == "" || !strings.HasPrefix(receivedAuth, "Basic ") {
		t.Fatalf("received auth = %q, want Basic auth", receivedAuth)
	}

	if receivedHeaders["Connection"] != "" {
		t.Fatal("Connection header should be stripped")
	}
}

func TestForwardHTTPSUsesSNIAndReplaysBytes(t *testing.T) {
	t.Parallel()

	clientHello := buildTLSClientHelloRecordForSNI("example.com")
	upstreamPayload := []byte("after-client-hello")
	downstreamPayload := []byte("proxy-response")

	var proxyState *connectProxyState
	proxyAddr, proxyDone, proxyState := startMockConnectProxy(t, http.StatusOK, func(conn net.Conn, reader *bufio.Reader, req *http.Request) {
		proxyState.connectTarget = req.Host
		proxyState.proxyAuth = req.Header.Get("Proxy-Authorization")

		replayed := make([]byte, len(clientHello))
		if _, err := io.ReadFull(reader, replayed); err != nil {
			proxyState.err = fmt.Errorf("read replayed client hello: %w", err)
			return
		}
		proxyState.replayed = replayed

		more := make([]byte, len(upstreamPayload))
		if _, err := io.ReadFull(reader, more); err != nil {
			proxyState.err = fmt.Errorf("read upstream payload: %w", err)
			return
		}
		proxyState.upstreamPayload = more

		if _, err := conn.Write(downstreamPayload); err != nil {
			proxyState.err = fmt.Errorf("write downstream payload: %w", err)
		}
	})
	defer proxyDone()

	d := NewProxyDialer(proxyAddr, "user", "pass", 0)
	clientConn, serverDone, serverErr := startForwardHTTPSServer(t, d, netip.MustParseAddrPort("203.0.113.10:443"), "fallback.example")
	defer clientConn.Close()

	if _, err := clientConn.Write(clientHello); err != nil {
		t.Fatalf("write client hello: %v", err)
	}
	if _, err := clientConn.Write(upstreamPayload); err != nil {
		t.Fatalf("write upstream payload: %v", err)
	}
	if tcpConn, ok := clientConn.(*net.TCPConn); ok {
		_ = tcpConn.CloseWrite()
	}

	gotResponse := make([]byte, len(downstreamPayload))
	if _, err := io.ReadFull(clientConn, gotResponse); err != nil {
		t.Fatalf("read downstream payload: %v", err)
	}
	if !bytes.Equal(gotResponse, downstreamPayload) {
		t.Fatalf("downstream payload = %q, want %q", gotResponse, downstreamPayload)
	}

	_ = clientConn.Close()
	<-serverDone
	if err := <-serverErr; err != nil {
		t.Fatalf("ForwardHTTPS() error = %v", err)
	}

	if proxyState.err != nil {
		t.Fatalf("mock proxy error = %v", proxyState.err)
	}
	if proxyState.connectTarget != "example.com:443" {
		t.Fatalf("CONNECT target = %q, want %q", proxyState.connectTarget, "example.com:443")
	}
	if proxyState.proxyAuth == "" || !strings.HasPrefix(proxyState.proxyAuth, "Basic ") {
		t.Fatalf("Proxy-Authorization = %q, want Basic auth", proxyState.proxyAuth)
	}
	if !bytes.Equal(proxyState.replayed, clientHello) {
		t.Fatalf("replayed bytes = %x, want %x", proxyState.replayed, clientHello)
	}
	if !bytes.Equal(proxyState.upstreamPayload, upstreamPayload) {
		t.Fatalf("upstream payload = %q, want %q", proxyState.upstreamPayload, upstreamPayload)
	}
}

func TestForwardHTTPSFallbacksWithoutSNI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		host       string
		dst        netip.AddrPort
		wantTarget string
	}{
		{
			name:       "uses provided host",
			host:       "provided.example:8443",
			dst:        netip.MustParseAddrPort("203.0.113.10:443"),
			wantTarget: "provided.example:443",
		},
		{
			name:       "uses destination ip when host missing",
			host:       "",
			dst:        netip.MustParseAddrPort("203.0.113.55:443"),
			wantTarget: "203.0.113.55:443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			clientHello := buildTLSClientHelloRecordForSNI("")
			var proxyState *connectProxyState
			proxyAddr, proxyDone, proxyState := startMockConnectProxy(t, http.StatusOK, func(conn net.Conn, reader *bufio.Reader, req *http.Request) {
				proxyState.connectTarget = req.Host
				replayed := make([]byte, len(clientHello))
				if _, err := io.ReadFull(reader, replayed); err != nil {
					proxyState.err = err
					return
				}
				proxyState.replayed = replayed
			})
			defer proxyDone()

			d := NewProxyDialer(proxyAddr, "", "", 0)
			clientConn, serverDone, serverErr := startForwardHTTPSServer(t, d, tt.dst, tt.host)
			defer clientConn.Close()

			if _, err := clientConn.Write(clientHello); err != nil {
				t.Fatalf("write client hello: %v", err)
			}
			if tcpConn, ok := clientConn.(*net.TCPConn); ok {
				_ = tcpConn.CloseWrite()
			}

			_ = clientConn.Close()
			<-serverDone
			if err := <-serverErr; err != nil {
				t.Fatalf("ForwardHTTPS() error = %v", err)
			}
			if proxyState.err != nil {
				t.Fatalf("mock proxy error = %v", proxyState.err)
			}
			if proxyState.connectTarget != tt.wantTarget {
				t.Fatalf("CONNECT target = %q, want %q", proxyState.connectTarget, tt.wantTarget)
			}
			if !bytes.Equal(proxyState.replayed, clientHello) {
				t.Fatalf("replayed bytes = %x, want %x", proxyState.replayed, clientHello)
			}
		})
	}
}

func TestForwardHTTPSRequires200ConnectResponse(t *testing.T) {
	t.Parallel()

	proxyAddr, proxyDone, _ := startMockConnectProxy(t, http.StatusBadGateway, nil)
	defer proxyDone()

	d := NewProxyDialer(proxyAddr, "", "", 0)
	clientConn, serverDone, serverErr := startForwardHTTPSServer(t, d, netip.MustParseAddrPort("203.0.113.10:443"), "")
	defer clientConn.Close()

	if _, err := clientConn.Write(buildTLSClientHelloRecordForSNI("")); err != nil {
		t.Fatalf("write client hello: %v", err)
	}
	_ = clientConn.Close()
	<-serverDone

	err := <-serverErr
	if err == nil || !strings.Contains(err.Error(), "proxy connect failed") {
		t.Fatalf("ForwardHTTPS() error = %v, want proxy connect failure", err)
	}
}

func mustParseURL(path string) *url.URL {
	u, err := url.Parse(path)
	if err != nil {
		panic(err)
	}
	return u
}

type mockConn struct {
	bytes.Buffer
}

func newMockConn() *mockConn {
	return &mockConn{}
}

func (m *mockConn) Close() error                       { return nil }
func (m *mockConn) LocalAddr() net.Addr                { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

type connectProxyState struct {
	connectTarget   string
	proxyAuth       string
	replayed        []byte
	upstreamPayload []byte
	err             error
}

func startMockConnectProxy(t *testing.T, statusCode int, afterConnect func(net.Conn, *bufio.Reader, *http.Request)) (string, func(), *connectProxyState) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen mock proxy: %v", err)
	}

	state := &connectProxyState{}
	done := make(chan struct{})

	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			state.err = err
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		req, err := http.ReadRequest(reader)
		if err != nil {
			state.err = fmt.Errorf("read connect request: %w", err)
			return
		}
		state.connectTarget = req.Host
		state.proxyAuth = req.Header.Get("Proxy-Authorization")

		statusText := http.StatusText(statusCode)
		if statusText == "" {
			statusText = "Status"
		}
		if _, err := fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\nContent-Length: 0\r\n\r\n", statusCode, statusText); err != nil {
			state.err = fmt.Errorf("write connect response: %w", err)
			return
		}

		if statusCode != http.StatusOK || afterConnect == nil {
			return
		}
		afterConnect(conn, reader, req)
	}()

	shutdown := func() {
		_ = listener.Close()
		<-done
	}

	return listener.Addr().String(), shutdown, state
}

func startForwardHTTPSServer(t *testing.T, dialer *ProxyDialer, dst netip.AddrPort, host string) (net.Conn, <-chan struct{}, <-chan error) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen forwarding server: %v", err)
	}

	serverDone := make(chan struct{})
	serverErr := make(chan error, 1)

	go func() {
		defer close(serverDone)
		defer listener.Close()

		conn, err := listener.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		serverErr <- dialer.ForwardHTTPS(ctx, conn, dst, host)
	}()

	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial forwarding server: %v", err)
	}

	return clientConn, serverDone, serverErr
}
