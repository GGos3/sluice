//go:build linux

package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strings"

	"github.com/ggos3/sluice/internal/rules"
)

var gatewayStackAddress = netip.MustParseAddr("10.0.85.2")

func Run(ctx context.Context, cfg *Config, log *slog.Logger) (runErr error) {
	if cfg == nil {
		return errors.New("config is required")
	}
	if ctx == nil {
		return errors.New("context is required")
	}
	if log == nil {
		log = slog.Default()
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	proxyIP, err := resolveProxyIPv4(ctx, cfg.ProxyHost)
	if err != nil {
		return fmt.Errorf("resolve proxy ipv4: %w", err)
	}

	stack, err := NewStack(cfg.TUNName, gatewayStackAddress)
	if err != nil {
		return fmt.Errorf("create stack: %w", err)
	}

	var closeErr error
	defer func() {
		if err := stack.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close stack: %w", err))
		}
		if runErr == nil {
			runErr = closeErr
		}
	}()

	routes := NewRouteManagerWithPolicy(cfg.RouteTable, cfg.RulePriority, cfg.Fwmark)
	if err := routes.Setup(stack.Name(), proxyIP); err != nil {
		return fmt.Errorf("setup routing: %w", err)
	}
	defer func() {
		if err := routes.Cleanup(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("cleanup routing: %w", err))
		}
		if runErr == nil {
			runErr = closeErr
		}
	}()

	dialer := NewProxyDialer(net.JoinHostPort(proxyIP.String(), fmt.Sprint(cfg.ProxyPort)), cfg.ProxyUser, cfg.ProxyPass, cfg.Fwmark)
	dialer.SetRulesEngine(rules.NewEngine(rules.Config{
		NoProxyDomains:  cfg.NoProxyDomains,
		NoProxyIPRanges: cfg.NoProxyIPRanges,
	}))

	if err := stack.ServeDNSOverHTTPS(netip.AddrPortFrom(netip.IPv4Unspecified(), 53), net.JoinHostPort(cfg.ProxyHost, fmt.Sprint(cfg.ProxyPort)), nil); err != nil {
		return fmt.Errorf("listen dns: %w", err)
	}

	httpListener, err := stack.ServeTCP(netip.AddrPortFrom(netip.IPv4Unspecified(), 80), func(conn net.Conn) {
		handleForward(log, conn, "", func(ctx context.Context, dst netip.AddrPort, host string) error {
			return dialer.ForwardHTTP(ctx, conn, dst, host)
		})
	})
	if err != nil {
		return fmt.Errorf("listen http: %w", err)
	}
	defer func() {
		if err := httpListener.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close http listener: %w", err))
		}
		if runErr == nil {
			runErr = closeErr
		}
	}()

	httpsListener, err := stack.ServeTCP(netip.AddrPortFrom(netip.IPv4Unspecified(), 443), func(conn net.Conn) {
		handleForward(log, conn, normalizeConnHost(conn.LocalAddr()), func(ctx context.Context, dst netip.AddrPort, host string) error {
			return dialer.ForwardHTTPS(ctx, conn, dst, host)
		})
	})
	if err != nil {
		return fmt.Errorf("listen https: %w", err)
	}
	defer func() {
		if err := httpsListener.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close https listener: %w", err))
		}
		if runErr == nil {
			runErr = closeErr
		}
	}()

	log.Debug("gateway started", "tun", stack.Name(), "proxy_ip", proxyIP.String())

	<-ctx.Done()
	if err := context.Cause(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("wait for shutdown: %w", err)
	}

	return nil
}

func handleForward(log *slog.Logger, conn net.Conn, host string, forward func(context.Context, netip.AddrPort, string) error) {
	defer conn.Close()

	dst, err := connAddrPort(conn.LocalAddr())
	if err != nil {
		log.Debug("gateway forward failed", "error", err)
		return
	}

	if err := forward(context.Background(), dst, host); err != nil {
		log.Debug("gateway forward failed", "dst", dst.String(), "error", err)
	}
}

func connAddrPort(addr net.Addr) (netip.AddrPort, error) {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		return netip.AddrPort{}, fmt.Errorf("unexpected tcp addr %T", addr)
	}
	ip, ok := netip.AddrFromSlice(tcpAddr.IP)
	if !ok {
		return netip.AddrPort{}, fmt.Errorf("invalid tcp addr %q", tcpAddr.String())
	}
	return netip.AddrPortFrom(ip, uint16(tcpAddr.Port)), nil
}

func normalizeConnHost(addr net.Addr) string {
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		return ""
	}
	host := tcpAddr.IP.String()
	if host == "" || host == "<nil>" {
		return ""
	}
	return net.JoinHostPort(strings.Trim(host, "[]"), fmt.Sprint(tcpAddr.Port))
}

func resolveProxyIPv4(ctx context.Context, host string) (net.IP, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, errors.New("proxy host is required")
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if !addr.Is4() {
			return nil, errors.New("proxy IP must be IPv4")
		}
		return net.IP(addr.AsSlice()), nil
	}

	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
	if err != nil {
		return nil, err
	}
	for _, addr := range addrs {
		if addr.Is4() {
			return net.IP(addr.AsSlice()), nil
		}
	}

	return nil, errors.New("no IPv4 address found")
}
