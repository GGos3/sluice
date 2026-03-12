package acl

import (
	"net"
	"strings"
	"sync"
)

type Whitelist struct {
	mu           sync.RWMutex
	rules        []rule
	enabled      bool
	dynamicDeny  []rule
	dynamicAllow []rule
}

type DynamicRule struct {
	Domain string
	Action string
}

type rule struct {
	domain            string
	subdomainsAllowed bool
}

func New(enabled bool, domains []string) *Whitelist {
	whitelist := &Whitelist{
		enabled: enabled,
		rules:   make([]rule, 0, len(domains)),
	}

	for _, domain := range domains {
		r, ok := parseRule(domain)
		if !ok {
			continue
		}

		whitelist.rules = append(whitelist.rules, r)
	}

	return whitelist
}

func parseRule(domain string) (rule, bool) {
	domain = normalizeRuleDomain(domain)
	if domain == "" {
		return rule{}, false
	}

	r := rule{domain: domain}
	if strings.HasPrefix(domain, "*.") {
		r.subdomainsAllowed = true
		r.domain = strings.TrimPrefix(domain, "*.")
	}

	if r.domain == "" {
		return rule{}, false
	}

	return r, true
}

func (w *Whitelist) IsAllowed(host string) bool {
	if w == nil {
		return false
	}

	w.mu.RLock()
	defer w.mu.RUnlock()

	if !w.enabled {
		host = normalizeHost(host)
		if host == "" {
			return true
		}

		for _, r := range w.dynamicDeny {
			if matchRule(r, host) {
				return false
			}
		}

		return true
	}

	host = normalizeHost(host)
	if host == "" {
		return false
	}

	for _, r := range w.dynamicDeny {
		if matchRule(r, host) {
			return false
		}
	}

	for _, r := range w.dynamicAllow {
		if matchRule(r, host) {
			return true
		}
	}

	for _, r := range w.rules {
		if matchRule(r, host) {
			return true
		}
	}

	return false
}

func matchRule(r rule, host string) bool {
	if !r.subdomainsAllowed {
		return host == r.domain
	}

	return strings.HasSuffix(host, "."+r.domain)
}

func (w *Whitelist) AddDeny(domain string) {
	if w == nil {
		return
	}

	r, ok := parseRule(domain)
	if !ok {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.dynamicDeny = append(w.dynamicDeny, r)
}

func (w *Whitelist) AddAllow(domain string) {
	if w == nil {
		return
	}

	r, ok := parseRule(domain)
	if !ok {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.dynamicAllow = append(w.dynamicAllow, r)
}

func (w *Whitelist) Remove(domain string) bool {
	if w == nil {
		return false
	}

	domain = normalizeRuleDomain(domain)
	if domain == "" {
		return false
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	found := false
	w.dynamicDeny = removeRule(w.dynamicDeny, domain, &found)
	w.dynamicAllow = removeRule(w.dynamicAllow, domain, &found)
	return found
}

func removeRule(rules []rule, domain string, found *bool) []rule {
	target := domain
	isSub := strings.HasPrefix(domain, "*.")
	if isSub {
		target = strings.TrimPrefix(domain, "*.")
	}

	result := make([]rule, 0, len(rules))
	for _, r := range rules {
		if r.domain == target && r.subdomainsAllowed == isSub {
			*found = true
			continue
		}
		result = append(result, r)
	}

	return result
}

func (w *Whitelist) DynamicRules() []DynamicRule {
	if w == nil {
		return nil
	}

	w.mu.RLock()
	defer w.mu.RUnlock()

	result := make([]DynamicRule, 0, len(w.dynamicDeny)+len(w.dynamicAllow))
	for _, r := range w.dynamicDeny {
		result = append(result, DynamicRule{Domain: formatRuleDomain(r), Action: "deny"})
	}
	for _, r := range w.dynamicAllow {
		result = append(result, DynamicRule{Domain: formatRuleDomain(r), Action: "allow"})
	}

	return result
}

func (w *Whitelist) StaticRules() []DynamicRule {
	if w == nil {
		return nil
	}

	w.mu.RLock()
	defer w.mu.RUnlock()

	result := make([]DynamicRule, 0, len(w.rules))
	for _, r := range w.rules {
		result = append(result, DynamicRule{Domain: formatRuleDomain(r), Action: "allow"})
	}

	return result
}

func formatRuleDomain(r rule) string {
	if r.subdomainsAllowed {
		return "*." + r.domain
	}

	return r.domain
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
