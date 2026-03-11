//go:build linux

package gateway

import (
	"errors"
	"net"
	"reflect"
	"testing"
)

func TestDetectLocalIPv4CIDRs(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	_, err := detectLocalIPv4CIDRs(func() ([]net.Interface, error) {
		return nil, boom
	})
	if err == nil {
		t.Fatal("detectLocalIPv4CIDRs() error = nil, want error")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("detectLocalIPv4CIDRs() error = %v, want wrapped boom", err)
	}
}

func TestDetectLocalIPv4CIDRsWithAddrs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		enumFn   func() ([]net.Interface, error)
		addrsFn  func(net.Interface) ([]net.Addr, error)
		want     []string
		wantErr  bool
		errCheck func(error) bool
	}{
		{
			name: "collects active ipv4 as host cidrs with dedup",
			enumFn: func() ([]net.Interface, error) {
				return []net.Interface{
					{Name: "eth0", Flags: net.FlagUp},
					{Name: "eth1", Flags: net.FlagUp},
					{Name: "eth2", Flags: net.FlagUp},
					{Name: "eth3", Flags: 0},
				}, nil
			},
			addrsFn: func(iface net.Interface) ([]net.Addr, error) {
				switch iface.Name {
				case "eth0":
					return []net.Addr{
						&net.IPNet{IP: net.ParseIP("192.0.2.10"), Mask: net.CIDRMask(24, 32)},
						&net.IPNet{IP: net.ParseIP("169.254.1.2"), Mask: net.CIDRMask(16, 32)},
						&net.IPNet{IP: net.ParseIP("2001:db8::10"), Mask: net.CIDRMask(64, 128)},
						&net.IPAddr{IP: net.ParseIP("198.51.100.7")},
						fakeAddr("ignored"),
					}, nil
				case "eth1":
					return []net.Addr{
						&net.IPNet{IP: net.ParseIP("203.0.113.5"), Mask: net.CIDRMask(32, 32)},
						&net.IPNet{IP: net.ParseIP("192.0.2.10"), Mask: net.CIDRMask(24, 32)},
					}, nil
				case "eth2":
					return nil, errors.New("addr lookup failed")
				case "eth3":
					return []net.Addr{&net.IPNet{IP: net.ParseIP("10.0.0.2"), Mask: net.CIDRMask(24, 32)}}, nil
				default:
					return nil, nil
				}
			},
			want: []string{"192.0.2.10/32", "198.51.100.7/32", "203.0.113.5/32"},
		},
		{
			name: "returns wrapped error when interface list fails",
			enumFn: func() ([]net.Interface, error) {
				return nil, errors.New("list failed")
			},
			addrsFn: func(net.Interface) ([]net.Addr, error) {
				return nil, nil
			},
			wantErr: true,
			errCheck: func(err error) bool {
				return err != nil && err.Error() == "list interfaces: list failed"
			},
		},
		{
			name: "requires enumerator",
			addrsFn: func(net.Interface) ([]net.Addr, error) {
				return nil, nil
			},
			wantErr: true,
			errCheck: func(err error) bool {
				return err != nil && err.Error() == "list interfaces: enumerator is required"
			},
		},
		{
			name: "requires addrs function",
			enumFn: func() ([]net.Interface, error) {
				return []net.Interface{}, nil
			},
			wantErr: true,
			errCheck: func(err error) bool {
				return err != nil && err.Error() == "list interfaces: addrs function is required"
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := detectLocalIPv4CIDRsWithAddrs(tt.enumFn, tt.addrsFn)
			if tt.wantErr {
				if err == nil {
					t.Fatal("detectLocalIPv4CIDRsWithAddrs() error = nil, want error")
				}
				if tt.errCheck != nil && !tt.errCheck(err) {
					t.Fatalf("detectLocalIPv4CIDRsWithAddrs() error = %v, errCheck failed", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("detectLocalIPv4CIDRsWithAddrs() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("detectLocalIPv4CIDRsWithAddrs() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

type fakeAddr string

func (a fakeAddr) Network() string { return "fake" }

func (a fakeAddr) String() string { return string(a) }
