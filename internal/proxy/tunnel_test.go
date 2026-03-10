package proxy

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ggos3/sluice/internal/acl"
)

func TestHandleConnectAllowedTunnel(t *testing.T) {
	targetAddr, shutdown := startTunnelTarget(t, false)
	defer shutdown()

	handler := NewHandler(acl.New(true, []string{"127.0.0.1"}), nil)
	proxyServer := httptest.NewServer(handler)
	t.Cleanup(proxyServer.Close)

	conn := connectThroughProxy(t, proxyServer.Listener.Addr().String(), targetAddr, "")
	defer conn.Close()

	buf := make([]byte, len("hello"))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read greeting: %v", err)
	}
	if got := string(buf); got != "hello" {
		t.Fatalf("greeting = %q, want hello", got)
	}

	if _, err := io.WriteString(conn, "ping"); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	buf = make([]byte, len("pong"))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if got := string(buf); got != "pong" {
		t.Fatalf("response = %q, want pong", got)
	}
}

func TestHandleConnectDenied(t *testing.T) {
	targetAddr, shutdown := startTunnelTarget(t, false)
	defer shutdown()

	handler := NewHandler(acl.New(true, []string{"example.com"}), nil)
	proxyServer := httptest.NewServer(handler)
	t.Cleanup(proxyServer.Close)

	conn, err := net.Dial("tcp", proxyServer.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", targetAddr, targetAddr); err != nil {
		t.Fatalf("write connect: %v", err)
	}

	statusLine := readStatusLine(t, conn)
	if !strings.Contains(statusLine, "403 Forbidden") {
		t.Fatalf("status line = %q, want 403 Forbidden", statusLine)
	}
}

func TestHandleConnectAuth(t *testing.T) {
	targetAddr, shutdown := startTunnelTarget(t, false)
	defer shutdown()

	handler := NewHandler(acl.New(true, []string{"127.0.0.1"}), nil, WithAuth(map[string]string{"user": "pass"}))
	proxyServer := httptest.NewServer(handler)
	t.Cleanup(proxyServer.Close)

	conn := connectThroughProxy(t, proxyServer.Listener.Addr().String(), targetAddr, formatBasicAuth("user", "pass"))
	defer conn.Close()

	if _, err := io.WriteString(conn, "ping"); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	buf := make([]byte, len("hello"))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read greeting: %v", err)
	}
	buf = make([]byte, len("pong"))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read pong: %v", err)
	}
}

func TestHandleConnectUnreachable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	handler := NewHandler(acl.New(true, []string{"127.0.0.1"}), nil)
	proxyServer := httptest.NewServer(handler)
	t.Cleanup(proxyServer.Close)

	conn, err := net.Dial("tcp", proxyServer.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()

	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", addr, addr); err != nil {
		t.Fatalf("write connect: %v", err)
	}

	statusLine := readStatusLine(t, conn)
	if !strings.Contains(statusLine, "502 Bad Gateway") {
		t.Fatalf("status line = %q, want 502 Bad Gateway", statusLine)
	}
}

func TestHandleConnectAccessLogging(t *testing.T) {
	var logs safeBuffer
	accessLogger := slog.New(slog.NewJSONHandler(&logs, nil))

	targetAddr, shutdown := startTunnelTarget(t, false)
	defer shutdown()

	handler := NewHandler(acl.New(true, []string{"127.0.0.1"}), accessLogger)
	proxyServer := httptest.NewServer(handler)
	t.Cleanup(proxyServer.Close)

	conn := connectThroughProxy(t, proxyServer.Listener.Addr().String(), targetAddr, "")
	if _, err := io.ReadFull(conn, make([]byte, len("hello"))); err != nil {
		t.Fatalf("read greeting: %v", err)
	}
	if _, err := io.WriteString(conn, "ping"); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	if _, err := io.ReadFull(conn, make([]byte, len("pong"))); err != nil {
		t.Fatalf("read pong: %v", err)
	}
	_ = conn.Close()

	time.Sleep(100 * time.Millisecond)

	entry := parseAccessLogLine(t, logs.String())
	proxy := entry["proxy"].(map[string]any)
	if proxy["method"] != http.MethodConnect {
		t.Fatalf("method = %v, want %s", proxy["method"], http.MethodConnect)
	}
	if proxy["domain"] != targetAddr {
		t.Fatalf("domain = %v, want %s", proxy["domain"], targetAddr)
	}
	if int(proxy["status"].(float64)) != http.StatusOK {
		t.Fatalf("status = %v, want %d", proxy["status"], http.StatusOK)
	}
	if proxy["allowed"] != true {
		t.Fatalf("allowed = %v, want true", proxy["allowed"])
	}
}

func startTunnelTarget(t *testing.T, closeImmediately bool) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen target: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go func(c net.Conn) {
				defer c.Close()
				if closeImmediately {
					return
				}
				_, _ = io.WriteString(c, "hello")
				buf := make([]byte, 4)
				if _, err := io.ReadFull(c, buf); err != nil {
					return
				}
				if string(buf) == "ping" {
					_, _ = io.WriteString(c, "pong")
				}
			}(conn)
		}
	}()

	return listener.Addr().String(), func() {
		_ = listener.Close()
		<-done
	}
}

func connectThroughProxy(t *testing.T, proxyAddr, targetAddr, authHeader string) net.Conn {
	t.Helper()
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}

	request := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", targetAddr, targetAddr)
	if authHeader != "" {
		request += fmt.Sprintf("Proxy-Authorization: %s\r\n", authHeader)
	}
	request += "\r\n"

	if _, err := io.WriteString(conn, request); err != nil {
		_ = conn.Close()
		t.Fatalf("write connect request: %v", err)
	}

	statusLine := readStatusLine(t, conn)
	if !strings.Contains(statusLine, "200 Connection Established") {
		_ = conn.Close()
		t.Fatalf("status line = %q, want 200 Connection Established", statusLine)
	}
	return conn
}

func readStatusLine(t *testing.T, conn net.Conn) string {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()

	var header bytes.Buffer
	one := make([]byte, 1)
	for {
		if _, err := conn.Read(one); err != nil {
			t.Fatalf("read connect response: %v", err)
		}
		header.WriteByte(one[0])
		if bytes.HasSuffix(header.Bytes(), []byte("\r\n\r\n")) {
			break
		}
	}

	statusLine, _, _ := strings.Cut(header.String(), "\r\n")
	return strings.TrimSpace(statusLine)
}
