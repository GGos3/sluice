//go:build linux

package gateway

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const dnsMessageContentType = "application/dns-message"

const defaultDNSRelayTimeout = 5 * time.Second

type dnsInterceptor struct {
	dohURL      string
	client      *http.Client
	controlMark int
}

func newDNSInterceptor(proxyAddr string, controlMark int, client *http.Client) *dnsInterceptor {
	proxyAddr = strings.TrimSpace(proxyAddr)
	proxyAddr = strings.TrimSuffix(proxyAddr, "/")
	if !strings.HasPrefix(proxyAddr, "http://") && !strings.HasPrefix(proxyAddr, "https://") {
		proxyAddr = "http://" + proxyAddr
	}

	if client == nil {
		transport := &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: defaultDNSRelayTimeout,
				Control: func(_, _ string, c syscall.RawConn) error {
					return c.Control(func(fd uintptr) {
						_ = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, controlMark)
					})
				},
			}).DialContext,
		}
		client = &http.Client{Timeout: defaultDNSRelayTimeout, Transport: transport}
	}

	return &dnsInterceptor{
		dohURL:      proxyAddr + "/dns-query",
		client:      client,
		controlMark: controlMark,
	}
}

func NewDNSRelayHandler(proxyAddr string, controlMark int, client *http.Client) DNSHandler {
	interceptor := newDNSInterceptor(proxyAddr, controlMark, client)
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
