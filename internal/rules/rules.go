package rules

import (
	"net"
	"strings"
)

type Rule interface {
	Match(domain string, ip net.IP) bool
}

type DomainRule struct {
	domain            string
	subdomainsAllowed bool
}

type IPRangeRule struct {
	network *net.IPNet
}

type Engine struct {
	rules []Rule
}

func NewEngine(cfg Config) *Engine {
	resolved := cfg.withDefaults()
	engine := &Engine{
		rules: make([]Rule, 0, len(resolved.NoProxyDomains)+len(resolved.NoProxyIPRanges)),
	}

	for _, domain := range resolved.NoProxyDomains {
		rule, ok := newDomainRule(domain)
		if !ok {
			continue
		}
		engine.rules = append(engine.rules, rule)
	}

	for _, rawCIDR := range resolved.NoProxyIPRanges {
		rule, ok := newIPRangeRule(rawCIDR)
		if !ok {
			continue
		}
		engine.rules = append(engine.rules, rule)
	}

	return engine
}

func (e *Engine) ShouldProxy(domain string, ip net.IP) bool {
	if e == nil {
		return true
	}

	normalizedDomain := normalizeHost(domain)
	normalizedIP := normalizeIP(ip)
	if normalizedIP == nil && normalizedDomain != "" {
		normalizedIP = normalizeIP(net.ParseIP(normalizedDomain))
	}

	for _, rule := range e.rules {
		if rule.Match(normalizedDomain, normalizedIP) {
			return false
		}
	}

	return true
}

func newDomainRule(domain string) (DomainRule, bool) {
	domain = normalizeRuleDomain(domain)
	if domain == "" {
		return DomainRule{}, false
	}

	rule := DomainRule{domain: domain}
	if strings.HasPrefix(domain, "*.") {
		rule.subdomainsAllowed = true
		rule.domain = strings.TrimPrefix(domain, "*.")
	}

	if rule.domain == "" {
		return DomainRule{}, false
	}

	return rule, true
}

func (r DomainRule) Match(domain string, _ net.IP) bool {
	domain = normalizeHost(domain)
	if domain == "" {
		return false
	}

	if !r.subdomainsAllowed {
		return domain == r.domain
	}

	return strings.HasSuffix(domain, "."+r.domain)
}

func newIPRangeRule(rawCIDR string) (IPRangeRule, bool) {
	_, network, err := net.ParseCIDR(strings.TrimSpace(rawCIDR))
	if err != nil {
		return IPRangeRule{}, false
	}

	return IPRangeRule{network: network}, true
}

func (r IPRangeRule) Match(_ string, ip net.IP) bool {
	ip = normalizeIP(ip)
	if ip == nil || r.network == nil {
		return false
	}

	return r.network.Contains(ip)
}

func normalizeRuleDomain(domain string) string {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if strings.HasPrefix(domain, "*.") {
		return "*." + strings.Trim(strings.TrimPrefix(domain, "*."), ".")
	}
	return strings.Trim(domain, ".")
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}

	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}

	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	}

	return strings.ToLower(strings.Trim(host, "."))
}

func normalizeIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}

	if ipv4 := ip.To4(); ipv4 != nil {
		return ipv4
	}

	return ip.To16()
}
