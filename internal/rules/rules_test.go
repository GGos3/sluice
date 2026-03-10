package rules

import (
	"net"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestEngineShouldProxy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cfg    Config
		domain string
		ip     net.IP
		want   bool
	}{
		{
			name:   "wildcard domain excludes subdomain",
			cfg:    Config{NoProxyDomains: []string{"*.internal.com"}},
			domain: "api.internal.com",
			want:   false,
		},
		{
			name:   "wildcard domain does not exclude apex",
			cfg:    Config{NoProxyDomains: []string{"*.internal.com"}},
			domain: "internal.com",
			want:   true,
		},
		{
			name:   "exact domain excludes apex",
			cfg:    Config{NoProxyDomains: []string{"internal.com"}},
			domain: "internal.com",
			want:   false,
		},
		{
			name:   "exact domain does not exclude subdomain",
			cfg:    Config{NoProxyDomains: []string{"internal.com"}},
			domain: "api.internal.com",
			want:   true,
		},
		{
			name:   "cidr excludes ipv4 range",
			cfg:    Config{NoProxyIPRanges: []string{"10.0.0.0/8"}},
			ip:     net.ParseIP("10.1.2.3"),
			want:   false,
		},
		{
			name:   "cidr does not exclude outside ipv4 range",
			cfg:    Config{NoProxyIPRanges: []string{"10.0.0.0/8"}},
			ip:     net.ParseIP("11.0.0.1"),
			want:   true,
		},
		{
			name:   "domain host with port is normalized",
			cfg:    Config{NoProxyDomains: []string{"*.internal.com"}},
			domain: "api.internal.com:443",
			want:   false,
		},
		{
			name:   "ip literal domain uses ip rules",
			cfg:    Config{NoProxyIPRanges: []string{"10.0.0.0/8"}},
			domain: "10.9.8.7",
			want:   false,
		},
		{
			name:   "mixed rules exclude when either matches",
			cfg:    Config{NoProxyDomains: []string{"*.internal.com"}, NoProxyIPRanges: []string{"10.0.0.0/8"}},
			domain: "example.com",
			ip:     net.ParseIP("10.2.3.4"),
			want:   false,
		},
		{
			name:   "unmatched traffic should proxy",
			cfg:    Config{NoProxyDomains: []string{"*.internal.com"}, NoProxyIPRanges: []string{"10.0.0.0/8"}},
			domain: "github.com",
			ip:     net.ParseIP("8.8.8.8"),
			want:   true,
		},
		{
			name:   "default localhost exclusion applies",
			cfg:    Config{},
			domain: "localhost",
			want:   false,
		},
		{
			name:   "default loopback exclusion applies",
			cfg:    Config{},
			ip:     net.ParseIP("127.0.0.1"),
			want:   false,
		},
		{
			name:   "default private range exclusion applies",
			cfg:    Config{},
			ip:     net.ParseIP("192.168.1.20"),
			want:   false,
		},
		{
			name:   "public traffic not excluded by defaults",
			cfg:    Config{},
			ip:     net.ParseIP("8.8.8.8"),
			want:   true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			engine := NewEngine(tt.cfg)
			if got := engine.ShouldProxy(tt.domain, tt.ip); got != tt.want {
				t.Fatalf("ShouldProxy(%q, %v) = %v, want %v", tt.domain, tt.ip, got, tt.want)
			}
		})
	}
}

func TestParseNoProxyCSV(t *testing.T) {
	t.Parallel()

	cfg := ParseNoProxyCSV("10.0.0.0/8, *.internal.com, internal.com, 192.168.0.0/16")

	if len(cfg.NoProxyDomains) != 2 {
		t.Fatalf("len(NoProxyDomains) = %d, want 2", len(cfg.NoProxyDomains))
	}
	if cfg.NoProxyDomains[0] != "*.internal.com" || cfg.NoProxyDomains[1] != "internal.com" {
		t.Fatalf("NoProxyDomains = %v, want [*.internal.com internal.com]", cfg.NoProxyDomains)
	}
	if len(cfg.NoProxyIPRanges) != 2 {
		t.Fatalf("len(NoProxyIPRanges) = %d, want 2", len(cfg.NoProxyIPRanges))
	}
	if cfg.NoProxyIPRanges[0] != "10.0.0.0/8" || cfg.NoProxyIPRanges[1] != "192.168.0.0/16" {
		t.Fatalf("NoProxyIPRanges = %v, want [10.0.0.0/8 192.168.0.0/16]", cfg.NoProxyIPRanges)
	}
}

func TestConfigUnmarshalYAML(t *testing.T) {
	t.Parallel()

	t.Run("supports yaml lists", func(t *testing.T) {
		t.Parallel()

		var cfg Config
		input := []byte("no_proxy_domains:\n  - '*.internal.com'\n  - 'internal.com'\nno_proxy_ip_ranges:\n  - '10.0.0.0/8'\n  - '192.168.0.0/16'\n")

		if err := yaml.Unmarshal(input, &cfg); err != nil {
			t.Fatalf("yaml.Unmarshal() error = %v", err)
		}

		if len(cfg.NoProxyDomains) != 2 || cfg.NoProxyDomains[0] != "*.internal.com" || cfg.NoProxyDomains[1] != "internal.com" {
			t.Fatalf("NoProxyDomains = %v, want [*.internal.com internal.com]", cfg.NoProxyDomains)
		}
		if len(cfg.NoProxyIPRanges) != 2 || cfg.NoProxyIPRanges[0] != "10.0.0.0/8" || cfg.NoProxyIPRanges[1] != "192.168.0.0/16" {
			t.Fatalf("NoProxyIPRanges = %v, want [10.0.0.0/8 192.168.0.0/16]", cfg.NoProxyIPRanges)
		}
	})

	t.Run("supports comma separated yaml scalars", func(t *testing.T) {
		t.Parallel()

		var cfg Config
		input := []byte("no_proxy_domains: '*.internal.com, internal.com'\nno_proxy_ip_ranges: '10.0.0.0/8, 192.168.0.0/16'\n")

		if err := yaml.Unmarshal(input, &cfg); err != nil {
			t.Fatalf("yaml.Unmarshal() error = %v", err)
		}

		if len(cfg.NoProxyDomains) != 2 || cfg.NoProxyDomains[0] != "*.internal.com" || cfg.NoProxyDomains[1] != "internal.com" {
			t.Fatalf("NoProxyDomains = %v, want [*.internal.com internal.com]", cfg.NoProxyDomains)
		}
		if len(cfg.NoProxyIPRanges) != 2 || cfg.NoProxyIPRanges[0] != "10.0.0.0/8" || cfg.NoProxyIPRanges[1] != "192.168.0.0/16" {
			t.Fatalf("NoProxyIPRanges = %v, want [10.0.0.0/8 192.168.0.0/16]", cfg.NoProxyIPRanges)
		}
	})
}

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()

	if len(cfg.NoProxyDomains) != 1 || cfg.NoProxyDomains[0] != "localhost" {
		t.Fatalf("NoProxyDomains = %v, want [localhost]", cfg.NoProxyDomains)
	}

	wantRanges := []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
	if len(cfg.NoProxyIPRanges) != len(wantRanges) {
		t.Fatalf("len(NoProxyIPRanges) = %d, want %d", len(cfg.NoProxyIPRanges), len(wantRanges))
	}
	for i, want := range wantRanges {
		if cfg.NoProxyIPRanges[i] != want {
			t.Fatalf("NoProxyIPRanges[%d] = %q, want %q", i, cfg.NoProxyIPRanges[i], want)
		}
	}
}

func TestNilEngineShouldProxy(t *testing.T) {
	t.Parallel()

	var engine *Engine
	if !engine.ShouldProxy("github.com", net.ParseIP("8.8.8.8")) {
		t.Fatal("nil engine should proxy by default")
	}
}
