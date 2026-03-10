package gateway

import (
	"flag"
	"strings"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     *Config
		wantErr string
	}{
		{
			name: "valid config with all defaults except ProxyHost",
			cfg: &Config{
				ProxyHost:    "127.0.0.1",
				ProxyPort:    8080,
				LogLevel:     "info",
				LogFormat:    "json",
				TUNName:      "sluice0",
				RouteTable:   100,
				RulePriority: 10010,
				Fwmark:       0x1,
			},
			wantErr: "",
		},
		{
			name: "valid config with proxy auth",
			cfg: &Config{
				ProxyHost: "192.168.1.100",
				ProxyPort: 3128,
				ProxyUser: "user1",
				ProxyPass: "secret",
				LogLevel:  "debug",
				LogFormat: "text",
			},
			wantErr: "",
		},
		{
			name: "valid config with domains",
			cfg: &Config{
				ProxyHost: "10.0.0.1",
				ProxyPort: 8080,
				Domains:   []string{"github.com", "*.github.com"},
				LogLevel:  "warn",
				LogFormat: "json",
			},
			wantErr: "",
		},
		{
			name: "empty proxy host",
			cfg: &Config{
				ProxyHost: "",
				ProxyPort: 8080,
			},
			wantErr: "proxy host is required",
		},
		{
			name: "whitespace proxy host",
			cfg: &Config{
				ProxyHost: "   ",
				ProxyPort: 8080,
			},
			wantErr: "proxy host is required",
		},
		{
			name: "port too low",
			cfg: &Config{
				ProxyHost: "127.0.0.1",
				ProxyPort: 0,
				LogLevel:  defaultLogLevel,
				LogFormat: defaultLogFormat,
			},
			wantErr: "proxy port must be between 1 and 65535",
		},
		{
			name: "port too high",
			cfg: &Config{
				ProxyHost: "127.0.0.1",
				ProxyPort: 70000,
				LogLevel:  defaultLogLevel,
				LogFormat: defaultLogFormat,
			},
			wantErr: "proxy port must be between 1 and 65535",
		},
		{
			name: "invalid log level",
			cfg: &Config{
				ProxyHost: "127.0.0.1",
				ProxyPort: 8080,
				LogLevel:  "trace",
				LogFormat: defaultLogFormat,
			},
			wantErr: "log level must be one of debug, info, warn, error",
		},
		{
			name: "invalid log format",
			cfg: &Config{
				ProxyHost: "127.0.0.1",
				ProxyPort: 8080,
				LogLevel:  defaultLogLevel,
				LogFormat: "yaml",
			},
			wantErr: "log format must be one of json, text",
		},
		{
			name: "port at minimum boundary",
			cfg: &Config{
				ProxyHost: "127.0.0.1",
				ProxyPort: 1,
				LogLevel:  defaultLogLevel,
				LogFormat: defaultLogFormat,
			},
			wantErr: "",
		},
		{
			name: "port at maximum boundary",
			cfg: &Config{
				ProxyHost: "127.0.0.1",
				ProxyPort: 65535,
				LogLevel:  defaultLogLevel,
				LogFormat: defaultLogFormat,
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.cfg.Validate()
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Validate() error = nil, want %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Validate() error = %q, want substring %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

func TestConfigDefault(t *testing.T) {
	t.Parallel()

	cfg := Default()

	if cfg.ProxyPort != defaultProxyPort {
		t.Fatalf("ProxyPort = %d, want %d", cfg.ProxyPort, defaultProxyPort)
	}
	if cfg.LogLevel != defaultLogLevel {
		t.Fatalf("LogLevel = %q, want %q", cfg.LogLevel, defaultLogLevel)
	}
	if cfg.LogFormat != defaultLogFormat {
		t.Fatalf("LogFormat = %q, want %q", cfg.LogFormat, defaultLogFormat)
	}
	if cfg.TUNName != defaultTUNName {
		t.Fatalf("TUNName = %q, want %q", cfg.TUNName, defaultTUNName)
	}
	if cfg.RouteTable != defaultRouteTable {
		t.Fatalf("RouteTable = %d, want %d", cfg.RouteTable, defaultRouteTable)
	}
	if cfg.RulePriority != defaultRulePriority {
		t.Fatalf("RulePriority = %d, want %d", cfg.RulePriority, defaultRulePriority)
	}
	if cfg.Fwmark != defaultFwmark {
		t.Fatalf("Fwmark = %d, want %d", cfg.Fwmark, defaultFwmark)
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Default() config should fail validation (ProxyHost is required)")
	}
}

func TestConfigProxyAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cfg      *Config
		wantAddr string
	}{
		{
			name: "standard address",
			cfg: &Config{
				ProxyHost: "192.168.1.100",
				ProxyPort: 8080,
			},
			wantAddr: "192.168.1.100:8080",
		},
		{
			name: "custom port",
			cfg: &Config{
				ProxyHost: "proxy.example.com",
				ProxyPort: 3128,
			},
			wantAddr: "proxy.example.com:3128",
		},
		{
			name: "localhost",
			cfg: &Config{
				ProxyHost: "127.0.0.1",
				ProxyPort: 8080,
			},
			wantAddr: "127.0.0.1:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.cfg.ProxyAddress()
			if got != tt.wantAddr {
				t.Fatalf("ProxyAddress() = %q, want %q", got, tt.wantAddr)
			}
		})
	}
}

func TestConfigNormalize(t *testing.T) {
	t.Parallel()

	t.Run("normalizes domain list", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			ProxyHost: "  127.0.0.1  ",
			ProxyUser: "  user  ",
			ProxyPass: "  pass  ",
			LogLevel:  "  DEBUG  ",
			LogFormat: "  JSON  ",
			TUNName:   "  sluice0  ",
			Domains:   []string{"  GITHUB.COM  ", "  *.Example.com  "},
		}

		cfg.normalize()

		if cfg.ProxyHost != "127.0.0.1" {
			t.Fatalf("ProxyHost = %q, want %q", cfg.ProxyHost, "127.0.0.1")
		}
		if cfg.ProxyUser != "user" {
			t.Fatalf("ProxyUser = %q, want %q", cfg.ProxyUser, "user")
		}
		if cfg.ProxyPass != "pass" {
			t.Fatalf("ProxyPass = %q, want %q", cfg.ProxyPass, "pass")
		}
		if cfg.LogLevel != "debug" {
			t.Fatalf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
		}
		if cfg.LogFormat != "json" {
			t.Fatalf("LogFormat = %q, want %q", cfg.LogFormat, "json")
		}
		if cfg.TUNName != "sluice0" {
			t.Fatalf("TUNName = %q, want %q", cfg.TUNName, "sluice0")
		}
		if len(cfg.Domains) != 2 || cfg.Domains[0] != "github.com" || cfg.Domains[1] != "*.example.com" {
			t.Fatalf("Domains = %v, want [github.com *.example.com]", cfg.Domains)
		}
	})
}

func TestNewConfigFromFlags(t *testing.T) {
	t.Parallel()

	t.Run("registers expected flags", func(t *testing.T) {
		t.Parallel()

		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		cfg := NewConfigFromFlags(fs)

		expectedFlags := []string{
			"proxy-host", "proxy-port", "proxy-user", "proxy-pass",
			"domains", "log-level", "log-format", "tun-name",
			"route-table", "rule-priority", "fwmark",
		}

		for _, name := range expectedFlags {
			if fs.Lookup(name) == nil {
				t.Fatalf("Flag %q not registered", name)
			}
		}

		if cfg.ProxyPort != defaultProxyPort {
			t.Fatalf("ProxyPort = %d, want default %d", cfg.ProxyPort, defaultProxyPort)
		}
		if cfg.LogLevel != defaultLogLevel {
			t.Fatalf("LogLevel = %q, want default %q", cfg.LogLevel, defaultLogLevel)
		}
		if cfg.TUNName != defaultTUNName {
			t.Fatalf("TUNName = %q, want default %q", cfg.TUNName, defaultTUNName)
		}
	})

	t.Run("domains flag parses comma-separated values", func(t *testing.T) {
		t.Parallel()

		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		cfg := NewConfigFromFlags(fs)

		err := fs.Set("domains", "github.com,*.github.com,pypi.org")
		if err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		if len(cfg.Domains) != 3 {
			t.Fatalf("Domains length = %d, want 3", len(cfg.Domains))
		}
		if cfg.Domains[0] != "github.com" || cfg.Domains[1] != "*.github.com" || cfg.Domains[2] != "pypi.org" {
			t.Fatalf("Domains = %v, want [github.com *.github.com pypi.org]", cfg.Domains)
		}
	})
}
