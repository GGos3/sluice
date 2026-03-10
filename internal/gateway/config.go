package gateway

import (
	"flag"
	"fmt"
	"strings"
)

const (
	defaultProxyPort    = 8080
	defaultLogLevel     = "info"
	defaultLogFormat    = "json"
	defaultTUNName      = "sluice0"
	defaultRouteTable   = 100
	defaultRulePriority = 10010
	defaultFwmark       = 0x1
)

var (
	validLogLevels = map[string]struct{}{
		"debug": {},
		"info":  {},
		"warn":  {},
		"error": {},
	}
	validLogFormats = map[string]struct{}{
		"json": {},
		"text": {},
	}
)

type stringListFlag []string

func (f *stringListFlag) Set(value string) error {
	if value == "" {
		*f = nil
		return nil
	}
	parts := strings.Split(value, ",")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	*f = parts
	return nil
}

func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

type Config struct {
	ProxyHost     string
	ProxyPort     int
	ProxyUser     string
	ProxyPass     string
	Domains       []string
	LogLevel      string
	LogFormat     string
	TUNName       string
	RouteTable    int
	RulePriority  int
	Fwmark        int
}

func Default() *Config {
	return &Config{
		ProxyPort:    defaultProxyPort,
		LogLevel:     defaultLogLevel,
		LogFormat:    defaultLogFormat,
		TUNName:      defaultTUNName,
		RouteTable:   defaultRouteTable,
		RulePriority: defaultRulePriority,
		Fwmark:       defaultFwmark,
	}
}

func (c *Config) Validate() error {
	// Normalize first to ensure validation works on cleaned input
	c.normalize()

	if strings.TrimSpace(c.ProxyHost) == "" {
		return fmt.Errorf("proxy host is required")
	}

	if c.ProxyPort < 1 || c.ProxyPort > 65535 {
		return fmt.Errorf("proxy port must be between 1 and 65535")
	}

	if _, ok := validLogLevels[c.LogLevel]; !ok {
		return fmt.Errorf("log level must be one of debug, info, warn, error")
	}

	if _, ok := validLogFormats[c.LogFormat]; !ok {
		return fmt.Errorf("log format must be one of json, text")
	}

	return nil
}

func NewConfigFromFlags(fs *flag.FlagSet) *Config {
	if fs == nil {
		fs = flag.CommandLine
	}

	cfg := Default()

	fs.StringVar(&cfg.ProxyHost, "proxy-host", "", "proxy server host (required)")
	fs.IntVar(&cfg.ProxyPort, "proxy-port", defaultProxyPort, "proxy server port")
	fs.StringVar(&cfg.ProxyUser, "proxy-user", "", "proxy authentication username")
	fs.StringVar(&cfg.ProxyPass, "proxy-pass", "", "proxy authentication password")
	fs.Var((*stringListFlag)(&cfg.Domains), "domains", "comma-separated list of domains to proxy (optional)")
	fs.StringVar(&cfg.LogLevel, "log-level", defaultLogLevel, "logging level (debug, info, warn, error)")
	fs.StringVar(&cfg.LogFormat, "log-format", defaultLogFormat, "logging format (json, text)")
	fs.StringVar(&cfg.TUNName, "tun-name", defaultTUNName, "TUN device name")
	fs.IntVar(&cfg.RouteTable, "route-table", defaultRouteTable, "routing table number")
	fs.IntVar(&cfg.RulePriority, "rule-priority", defaultRulePriority, "ip rule priority")
	fs.IntVar(&cfg.Fwmark, "fwmark", defaultFwmark, "fwmark value for routing bypass")

	return cfg
}

func (c *Config) ProxyAddress() string {
	return fmt.Sprintf("%s:%d", c.ProxyHost, c.ProxyPort)
}

func (c *Config) normalize() {
	c.ProxyHost = strings.TrimSpace(c.ProxyHost)
	c.ProxyUser = strings.TrimSpace(c.ProxyUser)
	c.ProxyPass = strings.TrimSpace(c.ProxyPass)
	c.LogLevel = strings.ToLower(strings.TrimSpace(c.LogLevel))
	c.LogFormat = strings.ToLower(strings.TrimSpace(c.LogFormat))
	c.TUNName = strings.TrimSpace(c.TUNName)

	for i, domain := range c.Domains {
		c.Domains[i] = strings.ToLower(strings.TrimSpace(domain))
	}
}
