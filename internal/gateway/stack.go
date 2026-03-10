//go:build linux

package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"sync/atomic"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/fdbased"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	gtcpipstack "gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

const (
	defaultStackMTU = 1420
	stackNICID      = 1
)

type TCPHandler func(net.Conn)
type DNSHandler func(context.Context, []byte) ([]byte, error)

type Stack struct {
	tun     *TUNDevice
	stack   *gtcpipstack.Stack
	closed  chan struct{}
	once    sync.Once
	rxCount atomic.Int64
	txCount atomic.Int64
}

func NewStack(name string, localAddr netip.Addr) (*Stack, error) {
	if name == "" {
		return nil, errors.New("tun name is required")
	}
	if !localAddr.IsValid() {
		return nil, errors.New("local address is required")
	}

	tun, err := NewTUN(name, defaultStackMTU)
	if err != nil {
		return nil, err
	}

	fd := tun.FD()
	mtu := tun.MTU()

	linkEP, err := fdbased.New(&fdbased.Options{FDs: []int{fd}, MTU: mtu})
	if err != nil {
		_ = tun.Close()
		return nil, fmt.Errorf("create fdbased endpoint: %w", err)
	}

	s := &Stack{
		tun: tun,
		stack: gtcpipstack.New(gtcpipstack.Options{
			NetworkProtocols:   []gtcpipstack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol},
			TransportProtocols: []gtcpipstack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
		}),
		closed: make(chan struct{}),
	}

	if err := s.stack.CreateNIC(stackNICID, linkEP); err != nil {
		_ = tun.Close()
		return nil, fmt.Errorf("create nic: %s", err)
	}

	if err := s.configure(localAddr); err != nil {
		s.Close()
		return nil, err
	}

	return s, nil
}

func (s *Stack) Close() error {
	if s == nil {
		return nil
	}
	var closeErr error
	s.once.Do(func() {
		if s.stack != nil {
			s.stack.RemoveNIC(stackNICID)
			s.stack.Close()
		}
		if s.tun != nil {
			closeErr = s.tun.Close()
		}
		close(s.closed)
	})
	return closeErr
}

func (s *Stack) Name() string {
	if s == nil {
		return ""
	}
	return s.tun.Name()
}

func (s *Stack) listenTCP(addr netip.AddrPort) (net.Listener, error) {
	var wq waiter.Queue
	ep, err := s.stack.NewEndpoint(tcp.ProtocolNumber, ipv4.ProtocolNumber, &wq)
	if err != nil {
		return nil, fmt.Errorf("new tcp endpoint for %s: %s", addr, err)
	}
	if err := ep.Bind(tcpip.FullAddress{Port: addr.Port()}); err != nil {
		ep.Close()
		return nil, fmt.Errorf("bind tcp %s: %s", addr, err)
	}
	if err := ep.Listen(10); err != nil {
		ep.Close()
		return nil, fmt.Errorf("listen tcp %s: %s", addr, err)
	}
	return gonet.NewTCPListener(s.stack, &wq, ep), nil
}

func (s *Stack) ServeTCP(addr netip.AddrPort, handler TCPHandler) (net.Listener, error) {
	if handler == nil {
		return nil, errors.New("tcp handler is required")
	}
	listener, err := s.listenTCP(addr)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handler(conn)
		}
	}()
	return listener, nil
}

func (s *Stack) serveTCP(addr netip.AddrPort, handler TCPHandler) (net.Listener, error) {
	return s.ServeTCP(addr, handler)
}

func (s *Stack) ServeDNS(addr netip.AddrPort, handler DNSHandler) error {
	if handler == nil {
		return errors.New("dns handler is required")
	}

	forwarder := udp.NewForwarder(s.stack, func(request *udp.ForwarderRequest) {
		id := request.ID()
		if id.LocalPort != addr.Port() {
			return
		}

		var wq waiter.Queue
		ep, tcpipErr := request.CreateEndpoint(&wq)
		if tcpipErr != nil {
			return
		}

		conn := gonet.NewUDPConn(&wq, ep)
		go func() {
			defer conn.Close()

			buffer := make([]byte, 65535)
			for {
				n, src, err := conn.ReadFrom(buffer)
				if err != nil {
					return
				}

				response, err := handler(context.Background(), append([]byte(nil), buffer[:n]...))
				if err != nil || len(response) == 0 {
					continue
				}

				if _, err := conn.WriteTo(response, src); err != nil {
					return
				}
			}
		}()
	})

	s.stack.SetTransportProtocolHandler(udp.ProtocolNumber, forwarder.HandlePacket)
	return nil
}

func (s *Stack) ServeDNSOverHTTPS(addr netip.AddrPort, proxyAddr string, client *http.Client) error {
	return s.ServeDNS(addr, NewDNSRelayHandler(proxyAddr, client))
}

func (s *Stack) debugCounts() (rx, tx int64) {
	if s == nil {
		return 0, 0
	}
	return s.rxCount.Load(), s.txCount.Load()
}

func (s *Stack) debugTCPStats() string {
	if s == nil || s.stack == nil {
		return ""
	}
	stats := s.stack.Stats().TCP
	return fmt.Sprintf("valid=%d invalid=%d checksum=%d sent=%d",
		stats.ValidSegmentsReceived.Value(),
		stats.InvalidSegmentsReceived.Value(),
		stats.ChecksumErrors.Value(),
		stats.SegmentsSent.Value(),
	)
}

func (s *Stack) debugLastRX() string {
	return "fdbased-path"
}

func (s *Stack) configure(localAddr netip.Addr) error {
	protocolAddr, route, err := protocolAddressAndRoute(localAddr)
	if err != nil {
		return err
	}
	if err := s.stack.AddProtocolAddress(stackNICID, protocolAddr, gtcpipstack.AddressProperties{}); err != nil {
		return fmt.Errorf("add protocol address %s: %s", localAddr, err)
	}
	s.stack.SetRouteTable([]tcpip.Route{route})
	return nil
}

func protocolAddressAndRoute(addr netip.Addr) (tcpip.ProtocolAddress, tcpip.Route, error) {
	if addr.Is4() {
		return tcpip.ProtocolAddress{
			Protocol:          ipv4.ProtocolNumber,
			AddressWithPrefix: tcpip.AddressWithPrefix{Address: tcpip.AddrFromSlice(addr.AsSlice()), PrefixLen: 30},
		}, tcpip.Route{Destination: header.IPv4EmptySubnet, NIC: stackNICID}, nil
	}
	if addr.Is6() {
		return tcpip.ProtocolAddress{
			Protocol:          ipv6.ProtocolNumber,
			AddressWithPrefix: tcpip.AddrFromSlice(addr.AsSlice()).WithPrefix(),
		}, tcpip.Route{Destination: header.IPv6EmptySubnet, NIC: stackNICID}, nil
	}
	return tcpip.ProtocolAddress{}, tcpip.Route{}, fmt.Errorf("unsupported local address %q", addr)
}
