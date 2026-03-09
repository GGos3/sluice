package acl

import (
	"net"
	"strings"
)

type Whitelist struct {
	rules   []rule
	enabled bool
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
		domain = normalizeRuleDomain(domain)
		if domain == "" {
			continue
		}

		r := rule{domain: domain}
		if strings.HasPrefix(domain, "*.") {
			r.subdomainsAllowed = true
			r.domain = strings.TrimPrefix(domain, "*.")
		}

		if r.domain == "" {
			continue
		}

		whitelist.rules = append(whitelist.rules, r)
	}

	return whitelist
}

func (w *Whitelist) IsAllowed(host string) bool {
	if w == nil {
		return false
	}

	if !w.enabled {
		return true
	}

	host = normalizeHost(host)
	if host == "" {
		return false
	}

	for _, r := range w.rules {
		if !r.subdomainsAllowed && host == r.domain {
			return true
		}

		if r.subdomainsAllowed && strings.HasSuffix(host, "."+r.domain) {
			return true
		}
	}

	return false
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
