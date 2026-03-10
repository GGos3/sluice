package rules

import (
	"fmt"
	"net"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	defaultNoProxyDomains = []string{"localhost"}
	defaultNoProxyIPRanges = []string{
		"127.0.0.0/8",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}
)

type Config struct {
	NoProxyDomains  []string `yaml:"no_proxy_domains"`
	NoProxyIPRanges []string `yaml:"no_proxy_ip_ranges"`
}

func DefaultConfig() Config {
	return Config{
		NoProxyDomains:  append([]string(nil), defaultNoProxyDomains...),
		NoProxyIPRanges: append([]string(nil), defaultNoProxyIPRanges...),
	}
}

func ParseNoProxyCSV(value string) Config {
	var cfg Config
	cfg.AppendNoProxyCSV(value)
	return cfg
}

func (c *Config) AppendNoProxyCSV(value string) {
	for _, part := range splitCommaSeparated(value) {
		if _, _, err := net.ParseCIDR(part); err == nil {
			c.NoProxyIPRanges = append(c.NoProxyIPRanges, part)
			continue
		}

		c.NoProxyDomains = append(c.NoProxyDomains, part)
	}

	c.normalize()
}

func (c Config) withDefaults() Config {
	merged := DefaultConfig()
	merged.NoProxyDomains = append(merged.NoProxyDomains, c.NoProxyDomains...)
	merged.NoProxyIPRanges = append(merged.NoProxyIPRanges, c.NoProxyIPRanges...)
	merged.normalize()
	return merged
}

func (c *Config) normalize() {
	c.NoProxyDomains = normalizeRuleDomains(c.NoProxyDomains)
	c.NoProxyIPRanges = normalizeIPRanges(c.NoProxyIPRanges)
}

func normalizeRuleDomains(domains []string) []string {
	normalized := make([]string, 0, len(domains))
	seen := make(map[string]struct{}, len(domains))

	for _, domain := range domains {
		domain = normalizeRuleDomain(domain)
		if domain == "" {
			continue
		}
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}
		normalized = append(normalized, domain)
	}

	return normalized
}

func normalizeIPRanges(ranges []string) []string {
	normalized := make([]string, 0, len(ranges))
	seen := make(map[string]struct{}, len(ranges))

	for _, raw := range ranges {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		_, network, err := net.ParseCIDR(raw)
		if err != nil {
			continue
		}

		cidr := network.String()
		if _, ok := seen[cidr]; ok {
			continue
		}
		seen[cidr] = struct{}{}
		normalized = append(normalized, cidr)
	}

	return normalized
}

func splitCommaSeparated(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		result = append(result, part)
	}

	return result
}

func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	type rawConfig struct {
		NoProxyDomains  yamlStringList `yaml:"no_proxy_domains"`
		NoProxyIPRanges yamlStringList `yaml:"no_proxy_ip_ranges"`
	}

	var raw rawConfig
	if err := node.Decode(&raw); err != nil {
		return err
	}

	c.NoProxyDomains = append([]string(nil), raw.NoProxyDomains...)
	c.NoProxyIPRanges = append([]string(nil), raw.NoProxyIPRanges...)
	c.normalize()
	return nil
}

type yamlStringList []string

func (l *yamlStringList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case 0:
		*l = nil
		return nil
	case yaml.SequenceNode:
		var values []string
		if err := node.Decode(&values); err != nil {
			return err
		}
		*l = splitCommaSeparated(strings.Join(values, ","))
		return nil
	case yaml.ScalarNode:
		*l = splitCommaSeparated(node.Value)
		return nil
	default:
		return fmt.Errorf("expected string or string list")
	}
}
