//go:build linux

package gateway

import (
	"fmt"
	"net"
	"sort"
)

func detectLocalIPv4CIDRs(enumFn func() ([]net.Interface, error)) ([]string, error) {
	return detectLocalIPv4CIDRsWithAddrs(enumFn, func(iface net.Interface) ([]net.Addr, error) {
		return iface.Addrs()
	})
}

func detectLocalIPv4CIDRsWithAddrs(enumFn func() ([]net.Interface, error), addrsFn func(net.Interface) ([]net.Addr, error)) ([]string, error) {
	if enumFn == nil {
		return nil, fmt.Errorf("list interfaces: enumerator is required")
	}
	if addrsFn == nil {
		return nil, fmt.Errorf("list interfaces: addrs function is required")
	}

	ifaces, err := enumFn()
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}

	seen := make(map[string]struct{})
	cidrs := make([]string, 0)

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := addrsFn(iface)
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ip := ipv4FromAddr(addr)
			if ip == nil || isIPv4LinkLocal(ip) {
				continue
			}

			cidr := (&net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)}).String()
			if _, ok := seen[cidr]; ok {
				continue
			}
			seen[cidr] = struct{}{}
			cidrs = append(cidrs, cidr)
		}
	}

	sort.Strings(cidrs)

	return cidrs, nil
}

func ipv4FromAddr(addr net.Addr) net.IP {
	var ip net.IP

	switch value := addr.(type) {
	case *net.IPNet:
		ip = value.IP
	case *net.IPAddr:
		ip = value.IP
	default:
		return nil
	}

	ip = ip.To4()
	if ip == nil {
		return nil
	}

	return append(net.IP(nil), ip...)
}

func isIPv4LinkLocal(ip net.IP) bool {
	ip = ip.To4()
	if ip == nil {
		return false
	}
	return ip[0] == 169 && ip[1] == 254
}
