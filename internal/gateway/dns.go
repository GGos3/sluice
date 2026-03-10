//go:build linux

package gateway

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const dnsMessageContentType = "application/dns-message"

const defaultDNSRelayTimeout = 5 * time.Second

type dnsInterceptor struct {
	dohURL string
	client *http.Client
}

func newDNSInterceptor(proxyAddr string, client *http.Client) *dnsInterceptor {
	proxyAddr = strings.TrimSpace(proxyAddr)
	proxyAddr = strings.TrimSuffix(proxyAddr, "/")
	if !strings.HasPrefix(proxyAddr, "http://") && !strings.HasPrefix(proxyAddr, "https://") {
		proxyAddr = "http://" + proxyAddr
	}

	if client == nil {
		client = &http.Client{Timeout: defaultDNSRelayTimeout}
	}

	return &dnsInterceptor{
		dohURL: proxyAddr + "/dns-query",
		client: client,
	}
}

func NewDNSRelayHandler(proxyAddr string, client *http.Client) DNSHandler {
	interceptor := newDNSInterceptor(proxyAddr, client)
	return interceptor.handleQuery
}

func (d *dnsInterceptor) handleQuery(ctx context.Context, query []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.dohURL, bytes.NewReader(query))
	if err != nil {
		return nil, fmt.Errorf("build doh request: %w", err)
	}
	req.Header.Set("Content-Type", dnsMessageContentType)
	req.Header.Set("Accept", dnsMessageContentType)

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do doh request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("doh request failed: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read doh response: %w", err)
	}

	return body, nil
}
