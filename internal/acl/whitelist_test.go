package acl

import "testing"

func TestWhitelistIsAllowed(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		domains []string
		host    string
		want    bool
	}{
		{
			name:    "exact domain match",
			enabled: true,
			domains: []string{"github.com"},
			host:    "github.com",
			want:    true,
		},
		{
			name:    "wildcard subdomain match",
			enabled: true,
			domains: []string{"*.github.com"},
			host:    "api.github.com",
			want:    true,
		},
		{
			name:    "deep subdomain match",
			enabled: true,
			domains: []string{"*.github.com"},
			host:    "a.b.github.com",
			want:    true,
		},
		{
			name:    "wildcard does not match apex",
			enabled: true,
			domains: []string{"*.github.com"},
			host:    "github.com",
			want:    false,
		},
		{
			name:    "case insensitive exact match",
			enabled: true,
			domains: []string{"github.com"},
			host:    "GitHub.COM",
			want:    true,
		},
		{
			name:    "host with port stripping",
			enabled: true,
			domains: []string{"github.com"},
			host:    "github.com:443",
			want:    true,
		},
		{
			name:    "denied domain",
			enabled: true,
			domains: []string{"github.com"},
			host:    "notallowed.com",
			want:    false,
		},
		{
			name:    "wildcard does not match sibling domain",
			enabled: true,
			domains: []string{"*.github.com"},
			host:    "notgithub.com",
			want:    false,
		},
		{
			name:    "empty host denied",
			enabled: true,
			domains: []string{"github.com"},
			host:    "",
			want:    false,
		},
		{
			name:    "whitelist disabled allows all",
			enabled: false,
			domains: []string{"github.com"},
			host:    "notallowed.com",
			want:    true,
		},
		{
			name:    "whitelist disabled allows empty host",
			enabled: false,
			domains: []string{"github.com"},
			host:    "",
			want:    true,
		},
		{
			name:    "ipv4 exact match",
			enabled: true,
			domains: []string{"192.168.1.10"},
			host:    "192.168.1.10",
			want:    true,
		},
		{
			name:    "ipv4 with port",
			enabled: true,
			domains: []string{"192.168.1.10"},
			host:    "192.168.1.10:8080",
			want:    true,
		},
		{
			name:    "ipv6 exact match",
			enabled: true,
			domains: []string{"::1"},
			host:    "::1",
			want:    true,
		},
		{
			name:    "ipv6 with brackets and port",
			enabled: true,
			domains: []string{"::1"},
			host:    "[::1]:443",
			want:    true,
		},
		{
			name:    "ipv6 with brackets no port",
			enabled: true,
			domains: []string{"::1"},
			host:    "[::1]",
			want:    true,
		},
		{
			name:    "multiple rules interaction exact and wildcard",
			enabled: true,
			domains: []string{"example.com", "*.github.com", "192.168.1.10"},
			host:    "api.github.com",
			want:    true,
		},
		{
			name:    "multiple rules still default deny when unmatched",
			enabled: true,
			domains: []string{"example.com", "*.github.com", "192.168.1.10"},
			host:    "gitlab.com",
			want:    false,
		},
		{
			name:    "wildcard rule normalized to lowercase",
			enabled: true,
			domains: []string{"*.GitHub.COM"},
			host:    "API.GITHUB.COM",
			want:    true,
		},
		{
			name:    "trailing dot is normalized",
			enabled: true,
			domains: []string{"github.com."},
			host:    "github.com.",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			whitelist := New(tt.enabled, tt.domains)
			if got := whitelist.IsAllowed(tt.host); got != tt.want {
				t.Fatalf("IsAllowed(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestNewSkipsEmptyRules(t *testing.T) {
	whitelist := New(true, []string{"", "   ", "*.", "*.github.com", "github.com"})

	if len(whitelist.rules) != 2 {
		t.Fatalf("len(rules) = %d, want 2", len(whitelist.rules))
	}
}
