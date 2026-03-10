//go:build linux && spike

package gateway

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/vishvananda/netlink"
)

func TestTUNNetstackHTTPSpike(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to create and configure a TUN device")
	}
	if _, err := os.Stat("/dev/net/tun"); err != nil {
		t.Skip("requires /dev/net/tun")
	}

	hostAddr := netip.MustParseAddr("10.200.0.1")
	stackAddr := netip.MustParseAddr("10.200.0.2")
	listenAddr := netip.AddrPortFrom(stackAddr, 18080)
	deviceName := fmt.Sprintf("slk%d", os.Getpid())

	stack, err := NewStack(deviceName, stackAddr)
	if err != nil {
		t.Fatalf("NewStack() error = %v", err)
	}
	defer func() {
		if err := stack.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	cleanupHost, err := configureHostSide(stack.Name(), hostAddr, stackAddr)
	if err != nil {
		t.Fatalf("configureHostSide() error = %v", err)
	}
	defer cleanupHost()

	listener, err := stack.serveTCP(listenAddr, func(conn net.Conn) {
		defer conn.Close()

		if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return
		}

		request, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			return
		}
		_ = request.Body.Close()

		response := &http.Response{
			StatusCode: http.StatusOK,
			ProtoMajor: 1,
			ProtoMinor: 1,
			Header: http.Header{
				"Content-Type":   []string{"text/plain"},
				"Content-Length": []string{fmt.Sprint(len("ok from tun"))},
			},
			Body: io.NopCloser(strings.NewReader("ok from tun")),
		}

		_ = response.Write(conn)
	})
	if err != nil {
		t.Fatalf("serveTCP() error = %v", err)
	}
	defer listener.Close()

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				LocalAddr: &net.TCPAddr{IP: net.IP(hostAddr.AsSlice())},
			}).DialContext,
		},
	}

	response, err := client.Get("http://" + listenAddr.String())
	if err != nil {
		rx, tx := stack.debugCounts()
		t.Fatalf("GET error = %v (rx=%d tx=%d last_rx=%q tcp_stats=%q)", err, rx, tx, stack.debugLastRX(), stack.debugTCPStats())
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	if response.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if string(body) != "ok from tun" {
		t.Fatalf("body = %q, want %q", string(body), "ok from tun")
	}
}

func configureHostSide(deviceName string, hostAddr, stackAddr netip.Addr) (func(), error) {
	link, err := lookupTUNLink(deviceName)
	if err != nil {
		return nil, fmt.Errorf("lookup link %q: %w", deviceName, err)
	}

	addr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   net.IP(hostAddr.AsSlice()),
			Mask: net.CIDRMask(hostAddr.BitLen(), hostAddr.BitLen()),
		},
		Peer: &net.IPNet{
			IP:   net.IP(stackAddr.AsSlice()),
			Mask: net.CIDRMask(stackAddr.BitLen(), stackAddr.BitLen()),
		},
	}

	if err := netlink.AddrAdd(link, addr); err != nil && !errors.Is(err, syscall.EEXIST) {
		return nil, fmt.Errorf("assign host address %s peer %s: %w", hostAddr, stackAddr, err)
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return nil, fmt.Errorf("bring link up: %w", err)
	}

	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Scope:     netlink.SCOPE_LINK,
		Dst: &net.IPNet{
			IP:   net.IP(stackAddr.AsSlice()),
			Mask: net.CIDRMask(stackAddr.BitLen(), stackAddr.BitLen()),
		},
	}
	if err := netlink.RouteAdd(route); err != nil && !errors.Is(err, syscall.EEXIST) {
		return nil, fmt.Errorf("add route to %s: %w", stackAddr, err)
	}

	return func() {
		_ = netlink.RouteDel(route)
		_ = netlink.LinkSetDown(link)
		_ = netlink.AddrDel(link, addr)
	}, nil
}

func lookupTUNLink(deviceName string) (netlink.Link, error) {
	var lastErr error

	for range 50 {
		link, err := netlink.LinkByName(deviceName)
		if err == nil {
			return link, nil
		}
		lastErr = err

		links, listErr := netlink.LinkList()
		if listErr == nil {
			for _, candidate := range links {
				if candidate == nil {
					continue
				}
				if candidate.Attrs() != nil && candidate.Attrs().Name == deviceName {
					return candidate, nil
				}
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	return nil, lastErr
}
