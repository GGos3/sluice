package acl

import (
	"fmt"
	"sync"
	"testing"
)

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

func TestAddDeny_BlocksDomain(t *testing.T) {
	w := New(false, nil)
	w.AddDeny("evil.com")
	if w.IsAllowed("evil.com") {
		t.Fatal("expected evil.com to be denied")
	}
	if !w.IsAllowed("good.com") {
		t.Fatal("expected good.com to be allowed")
	}
}

func TestAddAllow_AllowsDomain(t *testing.T) {
	w := New(true, []string{"existing.com"})
	if w.IsAllowed("new.com") {
		t.Fatal("expected new.com to be denied before AddAllow")
	}
	w.AddAllow("new.com")
	if !w.IsAllowed("new.com") {
		t.Fatal("expected new.com to be allowed after AddAllow")
	}
}

func TestDenyTakesPrecedenceOverStaticWhitelist(t *testing.T) {
	w := New(true, []string{"github.com"})
	if !w.IsAllowed("github.com") {
		t.Fatal("expected github.com to be allowed by static whitelist")
	}
	w.AddDeny("github.com")
	if w.IsAllowed("github.com") {
		t.Fatal("expected github.com to be denied after AddDeny")
	}
}

func TestDenyTakesPrecedenceOverDynamicAllow(t *testing.T) {
	w := New(true, nil)
	w.AddAllow("test.com")
	w.AddDeny("test.com")
	if w.IsAllowed("test.com") {
		t.Fatal("expected deny to take precedence over dynamic allow")
	}
}

func TestRemove_FromDeny(t *testing.T) {
	w := New(false, nil)
	w.AddDeny("evil.com")
	if w.IsAllowed("evil.com") {
		t.Fatal("expected evil.com denied")
	}
	if !w.Remove("evil.com") {
		t.Fatal("expected Remove to return true")
	}
	if !w.IsAllowed("evil.com") {
		t.Fatal("expected evil.com allowed after remove")
	}
}

func TestRemove_FromAllow(t *testing.T) {
	w := New(true, nil)
	w.AddAllow("new.com")
	if !w.IsAllowed("new.com") {
		t.Fatal("expected new.com allowed")
	}
	if !w.Remove("new.com") {
		t.Fatal("expected Remove to return true")
	}
	if w.IsAllowed("new.com") {
		t.Fatal("expected new.com denied after remove")
	}
}

func TestRemove_NotFound(t *testing.T) {
	w := New(false, nil)
	if w.Remove("nonexistent.com") {
		t.Fatal("expected Remove to return false for non-existent rule")
	}
}

func TestDynamicRules_Snapshot(t *testing.T) {
	w := New(true, []string{"static.com"})
	w.AddDeny("evil.com")
	w.AddAllow("good.com")
	rules := w.DynamicRules()
	if len(rules) != 2 {
		t.Fatalf("DynamicRules() len = %d, want 2", len(rules))
	}
	if rules[0].Domain != "evil.com" || rules[0].Action != "deny" {
		t.Fatalf("rules[0] = %+v, want {evil.com deny}", rules[0])
	}
	if rules[1].Domain != "good.com" || rules[1].Action != "allow" {
		t.Fatalf("rules[1] = %+v, want {good.com allow}", rules[1])
	}
}

func TestDynamicRules_Empty(t *testing.T) {
	w := New(true, []string{"static.com"})
	rules := w.DynamicRules()
	if len(rules) != 0 {
		t.Fatalf("DynamicRules() len = %d, want 0", len(rules))
	}
}

func TestStaticRules_ReturnsConfigRules(t *testing.T) {
	w := New(true, []string{"github.com", "*.example.com"})
	rules := w.StaticRules()
	if len(rules) != 2 {
		t.Fatalf("StaticRules() len = %d, want 2", len(rules))
	}
}

func TestConcurrentAccess(t *testing.T) {
	w := New(true, []string{"github.com"})
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(4)
		domain := fmt.Sprintf("domain%d.com", i)
		go func() { defer wg.Done(); w.AddDeny(domain) }()
		go func() { defer wg.Done(); w.AddAllow(domain) }()
		go func() { defer wg.Done(); w.IsAllowed(domain) }()
		go func() { defer wg.Done(); w.DynamicRules() }()
	}
	wg.Wait()
}

func TestNilWhitelist_DynamicMethods(t *testing.T) {
	var w *Whitelist
	w.AddDeny("test.com")
	w.AddAllow("test.com")
	w.Remove("test.com")
	w.DynamicRules()
	w.StaticRules()
}

func TestAddDeny_WildcardDomain(t *testing.T) {
	w := New(true, []string{"*.example.com"})
	if !w.IsAllowed("sub.example.com") {
		t.Fatal("expected sub.example.com allowed by static whitelist")
	}
	w.AddDeny("*.example.com")
	if w.IsAllowed("sub.example.com") {
		t.Fatal("expected sub.example.com denied after wildcard deny")
	}
}

func TestAddAllow_WildcardDomain(t *testing.T) {
	w := New(true, nil)
	w.AddAllow("*.trusted.com")
	if !w.IsAllowed("app.trusted.com") {
		t.Fatal("expected app.trusted.com allowed by wildcard allow")
	}
	if w.IsAllowed("trusted.com") {
		t.Fatal("wildcard should not match apex domain")
	}
}
