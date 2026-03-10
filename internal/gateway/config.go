package gateway

import (
	"flag"
	"fmt"
	"net"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultProxyPort    = 18080
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

type Config struct {
	ProxyHost        string
	ProxyPort        int
	ProxyUser        string
	ProxyPass        string
	NoProxyDomains   []string
	NoProxyIPRanges  []string
	LogLevel         string
	LogFormat        string
	TUNName          string
	RouteTable       int
	RulePriority     int
	Fwmark           int
	configFile       string
	noProxyValue     string
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
	c.normalize()

	if strings.TrimSpace(c.ProxyHost) != "" {
		if c.ProxyPort < 1 || c.ProxyPort > 65535 {
			return fmt.Errorf("proxy port must be between 1 and 65535")
		}
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

	fs.StringVar(&cfg.ProxyHost, "proxy-host", "", "proxy server host (optional for agent mode)")
	fs.IntVar(&cfg.ProxyPort, "proxy-port", defaultProxyPort, "proxy server port")
	fs.StringVar(&cfg.ProxyUser, "proxy-user", "", "proxy authentication username")
	fs.StringVar(&cfg.ProxyPass, "proxy-pass", "", "proxy authentication password")
	fs.StringVar(&cfg.noProxyValue, "no-proxy", "", "comma-separated list of domains and CIDRs to exclude from proxying (e.g., '10.0.0.0/8,*.internal.com')")
	fs.StringVar(&cfg.LogLevel, "log-level", defaultLogLevel, "logging level (debug, info, warn, error)")
	fs.StringVar(&cfg.LogFormat, "log-format", defaultLogFormat, "logging format (json, text)")
	fs.StringVar(&cfg.TUNName, "tun-name", defaultTUNName, "TUN device name")
	fs.IntVar(&cfg.RouteTable, "route-table", defaultRouteTable, "routing table number")
	fs.IntVar(&cfg.RulePriority, "rule-priority", defaultRulePriority, "ip rule priority")
	fs.IntVar(&cfg.Fwmark, "fwmark", defaultFwmark, "fwmark value for routing bypass")
	fs.StringVar(&cfg.configFile, "config", "", "path to YAML configuration file")

	return cfg
}

func PostProcessConfig(cfg *Config, fs *flag.FlagSet) error {
	if fs == nil {
		fs = flag.CommandLine
	}

	configFlag := fs.Lookup("config")
	if configFlag != nil && configFlag.Value.String() != "" {
		cfg.configFile = configFlag.Value.String()
		if err := loadYAMLConfig(cfg.configFile, cfg); err != nil {
			return err
		}
	}

	if cfg.noProxyValue != "" {
		parseNoProxyFlag(cfg, cfg.noProxyValue)
	}

	return nil
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

	for i, domain := range c.NoProxyDomains {
		c.NoProxyDomains[i] = strings.ToLower(strings.TrimSpace(domain))
	}

	for i, ipRange := range c.NoProxyIPRanges {
		c.NoProxyIPRanges[i] = strings.TrimSpace(ipRange)
	}
}

func parseNoProxyFlag(cfg *Config, value string) {
	if value == "" {
		return
	}

	parts := strings.Split(value, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		_, _, err := net.ParseCIDR(p)
		if err == nil {
			cfg.NoProxyIPRanges = append(cfg.NoProxyIPRanges, p)
		} else {
			cfg.NoProxyDomains = append(cfg.NoProxyDomains, p)
		}
	}
}

func loadYAMLConfig(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var yamlCfg struct {
		ProxyHost       string   `yaml:"proxy_host"`
		ProxyPort       int      `yaml:"proxy_port"`
		ProxyUser       string   `yaml:"proxy_user"`
		ProxyPass       string   `yaml:"proxy_pass"`
		NoProxyDomains  []string `yaml:"no_proxy_domains"`
		NoProxyIPRanges []string `yaml:"no_proxy_ip_ranges"`
		LogLevel        string   `yaml:"log_level"`
		LogFormat       string   `yaml:"log_format"`
		TUNName         string   `yaml:"tun_name"`
		RouteTable      int      `yaml:"route_table"`
		RulePriority    int      `yaml:"rule_priority"`
		Fwmark          int      `yaml:"fwmark"`
	}

	if err := yaml.Unmarshal(data, &yamlCfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	if yamlCfg.ProxyHost != "" {
		cfg.ProxyHost = yamlCfg.ProxyHost
	}
	if yamlCfg.ProxyPort != 0 {
		cfg.ProxyPort = yamlCfg.ProxyPort
	}
	if yamlCfg.ProxyUser != "" {
		cfg.ProxyUser = yamlCfg.ProxyUser
	}
	if yamlCfg.ProxyPass != "" {
		cfg.ProxyPass = yamlCfg.ProxyPass
	}
	if len(yamlCfg.NoProxyDomains) > 0 {
		cfg.NoProxyDomains = append(cfg.NoProxyDomains, yamlCfg.NoProxyDomains...)
	}
	if len(yamlCfg.NoProxyIPRanges) > 0 {
		cfg.NoProxyIPRanges = append(cfg.NoProxyIPRanges, yamlCfg.NoProxyIPRanges...)
	}
	if yamlCfg.LogLevel != "" {
		cfg.LogLevel = yamlCfg.LogLevel
	}
	if yamlCfg.LogFormat != "" {
		cfg.LogFormat = yamlCfg.LogFormat
	}
	if yamlCfg.TUNName != "" {
		cfg.TUNName = yamlCfg.TUNName
	}
	if yamlCfg.RouteTable != 0 {
		cfg.RouteTable = yamlCfg.RouteTable
	}
	if yamlCfg.RulePriority != 0 {
		cfg.RulePriority = yamlCfg.RulePriority
	}
	if yamlCfg.Fwmark != 0 {
		cfg.Fwmark = yamlCfg.Fwmark
	}

	return nil
}
