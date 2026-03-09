package config

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultHost         = "0.0.0.0"
	defaultPort         = 8080
	defaultReadTimeout  = 30
	defaultWriteTimeout = 30
	defaultIdleTimeout  = 120
	defaultLogLevel     = "info"
	defaultLogFormat    = "json"
	defaultAccessLog    = "stdout"
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
	Server    ServerConfig    `yaml:"server"`
	Whitelist WhitelistConfig `yaml:"whitelist"`
	Logging   LoggingConfig   `yaml:"logging"`
	Auth      AuthConfig      `yaml:"auth"`
}

type ServerConfig struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	ReadTimeout  int    `yaml:"read_timeout"`
	WriteTimeout int    `yaml:"write_timeout"`
	IdleTimeout  int    `yaml:"idle_timeout"`
}

type WhitelistConfig struct {
	Enabled bool     `yaml:"enabled"`
	Domains []string `yaml:"domains"`
}

type LoggingConfig struct {
	Level     string `yaml:"level"`
	Format    string `yaml:"format"`
	AccessLog string `yaml:"access_log"`
}

type AuthConfig struct {
	Enabled     bool         `yaml:"enabled"`
	Credentials []Credential `yaml:"credentials"`
}

type Credential struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.normalize()
	cfg.applyDefaults()

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Default returns a Config populated with sensible defaults.
// Whitelist and auth are disabled so the server starts without
// requiring domain lists or credentials.
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			Host:         defaultHost,
			Port:         defaultPort,
			ReadTimeout:  defaultReadTimeout,
			WriteTimeout: defaultWriteTimeout,
			IdleTimeout:  defaultIdleTimeout,
		},
		Whitelist: WhitelistConfig{
			Enabled: false,
		},
		Logging: LoggingConfig{
			Level:     defaultLogLevel,
			Format:    defaultLogFormat,
			AccessLog: defaultAccessLog,
		},
		Auth: AuthConfig{
			Enabled: false,
		},
	}
}

// Ensure loads config from path if it exists, or generates a default
// config file at path and returns the default config. The boolean
// return value is true when a new default file was created.
func Ensure(path string) (*Config, bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		cfg, loadErr := Load(path)
		return cfg, false, loadErr
	}

	if !errors.Is(err, os.ErrNotExist) {
		return nil, false, fmt.Errorf("stat config: %w", err)
	}

	cfg := Default()
	if err := writeDefault(path, cfg); err != nil {
		return nil, false, fmt.Errorf("generate default config: %w", err)
	}

	return cfg, true, nil
}

func writeDefault(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(cfg); err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func (c *Config) Address() string {
	return net.JoinHostPort(c.Server.Host, fmt.Sprintf("%d", c.Server.Port))
}

func (c *Config) applyDefaults() {
	if c.Server.Host == "" {
		c.Server.Host = defaultHost
	}
	if c.Server.Port == 0 {
		c.Server.Port = defaultPort
	}
	if c.Server.ReadTimeout == 0 {
		c.Server.ReadTimeout = defaultReadTimeout
	}
	if c.Server.WriteTimeout == 0 {
		c.Server.WriteTimeout = defaultWriteTimeout
	}
	if c.Server.IdleTimeout == 0 {
		c.Server.IdleTimeout = defaultIdleTimeout
	}
	if c.Logging.Level == "" {
		c.Logging.Level = defaultLogLevel
	}
	if c.Logging.Format == "" {
		c.Logging.Format = defaultLogFormat
	}
	if c.Logging.AccessLog == "" {
		c.Logging.AccessLog = defaultAccessLog
	}
}

func (c *Config) normalize() {
	c.Server.Host = strings.TrimSpace(c.Server.Host)
	c.Logging.Level = strings.ToLower(strings.TrimSpace(c.Logging.Level))
	c.Logging.Format = strings.ToLower(strings.TrimSpace(c.Logging.Format))
	c.Logging.AccessLog = strings.TrimSpace(c.Logging.AccessLog)

	for i, domain := range c.Whitelist.Domains {
		c.Whitelist.Domains[i] = strings.ToLower(strings.TrimSpace(domain))
	}

	for i, credential := range c.Auth.Credentials {
		c.Auth.Credentials[i].Username = strings.TrimSpace(credential.Username)
		c.Auth.Credentials[i].Password = strings.TrimSpace(credential.Password)
	}
}

func (c *Config) validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
	}

	if _, ok := validLogLevels[c.Logging.Level]; !ok {
		return fmt.Errorf("logging.level must be one of debug, info, warn, error")
	}

	if _, ok := validLogFormats[c.Logging.Format]; !ok {
		return fmt.Errorf("logging.format must be one of json, text")
	}

	if c.Whitelist.Enabled {
		if len(c.Whitelist.Domains) == 0 {
			return fmt.Errorf("whitelist.domains must not be empty when whitelist is enabled")
		}
		for _, domain := range c.Whitelist.Domains {
			if domain == "" {
				return fmt.Errorf("whitelist.domains must not contain empty values")
			}
		}
	}

	if c.Auth.Enabled {
		if len(c.Auth.Credentials) == 0 {
			return fmt.Errorf("auth.credentials must not be empty when auth is enabled")
		}
		for _, credential := range c.Auth.Credentials {
			if credential.Username == "" || credential.Password == "" {
				return fmt.Errorf("auth.credentials entries must include username and password")
			}
		}
	}

	return nil
}
