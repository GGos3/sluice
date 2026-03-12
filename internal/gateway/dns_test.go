//go:build linux

package gateway

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDNSInterceptorRelaysWireMessageBytes(t *testing.T) {
	t.Parallel()

	query := []byte{0x12, 0x34, 0x01, 0x00, 0x00, 0x01}
	wantResponse := []byte{0x12, 0x34, 0x81, 0x80, 0x00, 0x01}

	var gotMethod string
	var gotPath string
	var gotContentType string
	var gotAccept string
	var gotBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		gotAccept = r.Header.Get("Accept")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		gotBody = body

		w.Header().Set("Content-Type", dnsMessageContentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(wantResponse)
	}))
	defer server.Close()

	interceptor := newDNSInterceptor(server.URL, defaultControlMark, nil)

	response, err := interceptor.handleQuery(context.Background(), query)
	if err != nil {
		t.Fatalf("handleQuery() error = %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want %q", gotMethod, http.MethodPost)
	}
	if gotPath != "/dns-query" {
		t.Fatalf("path = %q, want %q", gotPath, "/dns-query")
	}
	if gotContentType != dnsMessageContentType {
		t.Fatalf("content-type = %q, want %q", gotContentType, dnsMessageContentType)
	}
	if gotAccept != dnsMessageContentType {
		t.Fatalf("accept = %q, want %q", gotAccept, dnsMessageContentType)
	}
	if !bytes.Equal(gotBody, query) {
		t.Fatalf("request body = %x, want %x", gotBody, query)
	}
	if !bytes.Equal(response, wantResponse) {
		t.Fatalf("response = %x, want %x", response, wantResponse)
	}
}

func TestDNSInterceptorTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte{0x00})
	}))
	defer server.Close()

	client := &http.Client{Timeout: 20 * time.Millisecond}
	interceptor := newDNSInterceptor(server.URL, defaultControlMark, client)

	_, err := interceptor.handleQuery(context.Background(), []byte{0x01, 0x02})
	if err == nil {
		t.Fatal("handleQuery() error = nil, want timeout error")
	}
	if !strings.Contains(err.Error(), "do doh request") {
		t.Fatalf("error = %v, want wrapped doh request error", err)
	}
}

func TestDNSInterceptorHTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer server.Close()

	interceptor := newDNSInterceptor(server.URL, defaultControlMark, nil)

	_, err := interceptor.handleQuery(context.Background(), []byte{0x01, 0x02})
	if err == nil {
		t.Fatal("handleQuery() error = nil, want non-200 error")
	}
	if !strings.Contains(err.Error(), "doh request failed") {
		t.Fatalf("error = %v, want doh request failed", err)
	}
}
