package gateway

import (
	"flag"
	"os"
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
			name: "valid config with all defaults",
			cfg: &Config{
				ProxyPort:     18080,
				LogLevel:      "info",
				LogFormat:     "json",
				TUNName:       "sluice0",
				RouteTable:    100,
				RulePriority:  10010,
				Fwmark:        0x1,
				ControlFwmark: 0x2,
			},
			wantErr: "",
		},
		{
			name: "valid config with proxy host",
			cfg: &Config{
				ProxyHost:     "127.0.0.1",
				ProxyPort:     18080,
				LogLevel:      "info",
				LogFormat:     "json",
				TUNName:       "sluice0",
				RouteTable:    100,
				RulePriority:  10010,
				Fwmark:        0x1,
				ControlFwmark: 0x2,
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
			name: "valid config with no-proxy fields",
			cfg: &Config{
				ProxyHost:       "10.0.0.1",
				ProxyPort:       8080,
				NoProxyDomains:  []string{"github.com", "*.github.com"},
				NoProxyIPRanges: []string{"10.0.0.0/8", "192.168.0.0/16"},
				LogLevel:        "warn",
				LogFormat:       "json",
			},
			wantErr: "",
		},
		{
			name: "empty proxy host is allowed for agent mode",
			cfg: &Config{
				ProxyHost: "",
				ProxyPort: 8080,
				LogLevel:  defaultLogLevel,
				LogFormat: defaultLogFormat,
			},
			wantErr: "",
		},
		{
			name: "whitespace proxy host is allowed for agent mode",
			cfg: &Config{
				ProxyHost: "   ",
				ProxyPort: 8080,
				LogLevel:  defaultLogLevel,
				LogFormat: defaultLogFormat,
			},
			wantErr: "",
		},
		{
			name: "port too low",
			cfg: &Config{
				ProxyPort: 0,
				LogLevel:  defaultLogLevel,
				LogFormat: defaultLogFormat,
			},
			wantErr: "proxy port must be between 1 and 65535",
		},
		{
			name: "port too high",
			cfg: &Config{
				ProxyPort: 70000,
				LogLevel:  defaultLogLevel,
				LogFormat: defaultLogFormat,
			},
			wantErr: "proxy port must be between 1 and 65535",
		},
		{
			name: "invalid log level",
			cfg: &Config{
				ProxyPort: 8080,
				LogLevel:  "trace",
				LogFormat: defaultLogFormat,
			},
			wantErr: "log level must be one of debug, info, warn, error",
		},
		{
			name: "invalid log format",
			cfg: &Config{
				ProxyPort: 8080,
				LogLevel:  defaultLogLevel,
				LogFormat: "yaml",
			},
			wantErr: "log format must be one of json, text",
		},
		{
			name: "port at minimum boundary",
			cfg: &Config{
				ProxyPort: 1,
				LogLevel:  defaultLogLevel,
				LogFormat: defaultLogFormat,
			},
			wantErr: "",
		},
		{
			name: "port at maximum boundary",
			cfg: &Config{
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
	if cfg.ControlFwmark != defaultControlMark {
		t.Fatalf("ControlFwmark = %d, want %d", cfg.ControlFwmark, defaultControlMark)
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Default() config should pass validation, got error: %v", err)
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

	t.Run("normalizes no-proxy domain list", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			ProxyHost:       "  127.0.0.1  ",
			ProxyUser:       "  user  ",
			ProxyPass:       "  pass  ",
			LogLevel:        "  DEBUG  ",
			LogFormat:       "  JSON  ",
			TUNName:         "  sluice0  ",
			NoProxyDomains:  []string{"  GITHUB.COM  ", "  *.Example.com  "},
			NoProxyIPRanges: []string{"  10.0.0.0/8  "},
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
		if len(cfg.NoProxyDomains) != 2 || cfg.NoProxyDomains[0] != "github.com" || cfg.NoProxyDomains[1] != "*.example.com" {
			t.Fatalf("NoProxyDomains = %v, want [github.com *.example.com]", cfg.NoProxyDomains)
		}
		if len(cfg.NoProxyIPRanges) != 1 || cfg.NoProxyIPRanges[0] != "10.0.0.0/8" {
			t.Fatalf("NoProxyIPRanges = %v, want [10.0.0.0/8]", cfg.NoProxyIPRanges)
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
			"no-proxy", "log-level", "log-format", "tun-name",
			"route-table", "rule-priority", "fwmark", "control-fwmark", "config",
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

	t.Run("no-proxy flag parses mixed CIDR and domain values", func(t *testing.T) {
		t.Parallel()

		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		cfg := NewConfigFromFlags(fs)

		err := fs.Set("no-proxy", "10.0.0.0/8,*.internal.com,192.168.0.0/16,api.example.com")
		if err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		err = PostProcessConfig(cfg, fs)
		if err != nil {
			t.Fatalf("PostProcessConfig() error = %v", err)
		}

		if len(cfg.NoProxyIPRanges) != 2 {
			t.Fatalf("NoProxyIPRanges length = %d, want 2", len(cfg.NoProxyIPRanges))
		}
		if cfg.NoProxyIPRanges[0] != "10.0.0.0/8" {
			t.Fatalf("NoProxyIPRanges[0] = %q, want 10.0.0.0/8", cfg.NoProxyIPRanges[0])
		}
		if cfg.NoProxyIPRanges[1] != "192.168.0.0/16" {
			t.Fatalf("NoProxyIPRanges[1] = %q, want 192.168.0.0/16", cfg.NoProxyIPRanges[1])
		}

		if len(cfg.NoProxyDomains) != 2 {
			t.Fatalf("NoProxyDomains length = %d, want 2", len(cfg.NoProxyDomains))
		}
		if cfg.NoProxyDomains[0] != "*.internal.com" {
			t.Fatalf("NoProxyDomains[0] = %q, want *.internal.com", cfg.NoProxyDomains[0])
		}
		if cfg.NoProxyDomains[1] != "api.example.com" {
			t.Fatalf("NoProxyDomains[1] = %q, want api.example.com", cfg.NoProxyDomains[1])
		}
	})
}

func TestLoadYAMLConfig(t *testing.T) {
	t.Parallel()

	t.Run("loads YAML config file", func(t *testing.T) {
		t.Parallel()

		content := `
proxy_host: "192.168.1.100"
proxy_port: 3128
proxy_user: "user1"
proxy_pass: "secret"
no_proxy_domains:
  - "*.internal.com"
  - "localhost"
no_proxy_ip_ranges:
  - "10.0.0.0/8"
  - "172.16.0.0/12"
log_level: "debug"
log_format: "text"
tun_name: "sluice1"
route_table: 200
rule_priority: 10020
fwmark: 0x2
control_fwmark: 0x3
`
		tmpFile, err := os.CreateTemp("", "sluice-config-*.yaml")
		if err != nil {
			t.Fatalf("CreateTemp() error = %v", err)
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.WriteString(content); err != nil {
			t.Fatalf("WriteString() error = %v", err)
		}
		tmpFile.Close()

		cfg := Default()
		if err := loadYAMLConfig(tmpFile.Name(), cfg); err != nil {
			t.Fatalf("loadYAMLConfig() error = %v", err)
		}

		if cfg.ProxyHost != "192.168.1.100" {
			t.Fatalf("ProxyHost = %q, want %q", cfg.ProxyHost, "192.168.1.100")
		}
		if cfg.ProxyPort != 3128 {
			t.Fatalf("ProxyPort = %d, want %d", cfg.ProxyPort, 3128)
		}
		if cfg.ProxyUser != "user1" {
			t.Fatalf("ProxyUser = %q, want %q", cfg.ProxyUser, "user1")
		}
		if cfg.ProxyPass != "secret" {
			t.Fatalf("ProxyPass = %q, want %q", cfg.ProxyPass, "secret")
		}
		if len(cfg.NoProxyDomains) != 2 {
			t.Fatalf("NoProxyDomains length = %d, want 2", len(cfg.NoProxyDomains))
		}
		if cfg.NoProxyDomains[0] != "*.internal.com" {
			t.Fatalf("NoProxyDomains[0] = %q, want %q", cfg.NoProxyDomains[0], "*.internal.com")
		}
		if len(cfg.NoProxyIPRanges) != 2 {
			t.Fatalf("NoProxyIPRanges length = %d, want 2", len(cfg.NoProxyIPRanges))
		}
		if cfg.NoProxyIPRanges[0] != "10.0.0.0/8" {
			t.Fatalf("NoProxyIPRanges[0] = %q, want %q", cfg.NoProxyIPRanges[0], "10.0.0.0/8")
		}
		if cfg.LogLevel != "debug" {
			t.Fatalf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
		}
		if cfg.LogFormat != "text" {
			t.Fatalf("LogFormat = %q, want %q", cfg.LogFormat, "text")
		}
		if cfg.TUNName != "sluice1" {
			t.Fatalf("TUNName = %q, want %q", cfg.TUNName, "sluice1")
		}
		if cfg.ControlFwmark != 0x3 {
			t.Fatalf("ControlFwmark = %d, want %d", cfg.ControlFwmark, 0x3)
		}
	})

	t.Run("handles missing config file", func(t *testing.T) {
		t.Parallel()

		cfg := Default()
		err := loadYAMLConfig("/nonexistent/path/config.yaml", cfg)
		if err == nil {
			t.Fatal("loadYAMLConfig() expected error for missing file")
		}
		if !strings.Contains(err.Error(), "read config") {
			t.Fatalf("Error = %q, want substring 'read config'", err.Error())
		}
	})

	t.Run("handles invalid YAML", func(t *testing.T) {
		t.Parallel()

		tmpFile, err := os.CreateTemp("", "sluice-config-*.yaml")
		if err != nil {
			t.Fatalf("CreateTemp() error = %v", err)
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.WriteString("invalid: yaml: content: ["); err != nil {
			t.Fatalf("WriteString() error = %v", err)
		}
		tmpFile.Close()

		cfg := Default()
		err = loadYAMLConfig(tmpFile.Name(), cfg)
		if err == nil {
			t.Fatal("loadYAMLConfig() expected error for invalid YAML")
		}
		if !strings.Contains(err.Error(), "parse config") {
			t.Fatalf("Error = %q, want substring 'parse config'", err.Error())
		}
	})

	t.Run("merges YAML config with existing values", func(t *testing.T) {
		t.Parallel()

		content := `
proxy_host: "192.168.1.100"
no_proxy_domains:
  - "*.internal.com"
`
		tmpFile, err := os.CreateTemp("", "sluice-config-*.yaml")
		if err != nil {
			t.Fatalf("CreateTemp() error = %v", err)
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.WriteString(content); err != nil {
			t.Fatalf("WriteString() error = %v", err)
		}
		tmpFile.Close()

		cfg := &Config{
			ProxyHost:      "10.0.0.1",
			ProxyPort:      8080,
			NoProxyDomains: []string{"localhost"},
		}

		if err := loadYAMLConfig(tmpFile.Name(), cfg); err != nil {
			t.Fatalf("loadYAMLConfig() error = %v", err)
		}

		if cfg.ProxyHost != "192.168.1.100" {
			t.Fatalf("ProxyHost = %q, want %q", cfg.ProxyHost, "192.168.1.100")
		}
		if cfg.ProxyPort != 8080 {
			t.Fatalf("ProxyPort = %d, want %d (should not be overwritten)", cfg.ProxyPort, 8080)
		}
		if len(cfg.NoProxyDomains) != 2 {
			t.Fatalf("NoProxyDomains length = %d, want 2 (merged)", len(cfg.NoProxyDomains))
		}
	})
}

func TestPostProcessConfig(t *testing.T) {
	t.Parallel()

	t.Run("processes --config flag after parse", func(t *testing.T) {
		t.Parallel()

		content := `
proxy_host: "192.168.1.100"
proxy_port: 3128
no_proxy_domains:
  - "*.internal.com"
`
		tmpFile, err := os.CreateTemp("", "sluice-config-*.yaml")
		if err != nil {
			t.Fatalf("CreateTemp() error = %v", err)
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.WriteString(content); err != nil {
			t.Fatalf("WriteString() error = %v", err)
		}
		tmpFile.Close()

		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		cfg := NewConfigFromFlags(fs)

		err = fs.Set("config", tmpFile.Name())
		if err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		err = PostProcessConfig(cfg, fs)
		if err != nil {
			t.Fatalf("PostProcessConfig() error = %v", err)
		}

		if cfg.ProxyHost != "192.168.1.100" {
			t.Fatalf("ProxyHost = %q, want %q", cfg.ProxyHost, "192.168.1.100")
		}
		if cfg.ProxyPort != 3128 {
			t.Fatalf("ProxyPort = %d, want %d", cfg.ProxyPort, 3128)
		}
		if len(cfg.NoProxyDomains) != 1 || cfg.NoProxyDomains[0] != "*.internal.com" {
			t.Fatalf("NoProxyDomains = %v, want [*.internal.com]", cfg.NoProxyDomains)
		}
	})

	t.Run("processes --no-proxy flag after parse", func(t *testing.T) {
		t.Parallel()

		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		cfg := NewConfigFromFlags(fs)

		err := fs.Set("no-proxy", "10.0.0.0/8,*.internal.com")
		if err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		err = PostProcessConfig(cfg, fs)
		if err != nil {
			t.Fatalf("PostProcessConfig() error = %v", err)
		}

		if len(cfg.NoProxyIPRanges) != 1 || cfg.NoProxyIPRanges[0] != "10.0.0.0/8" {
			t.Fatalf("NoProxyIPRanges = %v, want [10.0.0.0/8]", cfg.NoProxyIPRanges)
		}
		if len(cfg.NoProxyDomains) != 1 || cfg.NoProxyDomains[0] != "*.internal.com" {
			t.Fatalf("NoProxyDomains = %v, want [*.internal.com]", cfg.NoProxyDomains)
		}
	})

	t.Run("returns error for missing config file", func(t *testing.T) {
		t.Parallel()

		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		cfg := NewConfigFromFlags(fs)

		err := fs.Set("config", "/nonexistent/path/config.yaml")
		if err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		err = PostProcessConfig(cfg, fs)
		if err == nil {
			t.Fatal("PostProcessConfig() expected error for missing file")
		}
		if !strings.Contains(err.Error(), "read config") {
			t.Fatalf("Error = %q, want substring 'read config'", err.Error())
		}
	})

	t.Run("handles both config and no-proxy flags", func(t *testing.T) {
		t.Parallel()

		content := `
proxy_host: "192.168.1.100"
no_proxy_domains:
  - "localhost"
no_proxy_ip_ranges:
  - "172.16.0.0/12"
`
		tmpFile, err := os.CreateTemp("", "sluice-config-*.yaml")
		if err != nil {
			t.Fatalf("CreateTemp() error = %v", err)
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.WriteString(content); err != nil {
			t.Fatalf("WriteString() error = %v", err)
		}
		tmpFile.Close()

		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		cfg := NewConfigFromFlags(fs)

		err = fs.Set("config", tmpFile.Name())
		if err != nil {
			t.Fatalf("Set() error = %v", err)
		}
		err = fs.Set("no-proxy", "10.0.0.0/8,*.example.com")
		if err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		err = PostProcessConfig(cfg, fs)
		if err != nil {
			t.Fatalf("PostProcessConfig() error = %v", err)
		}

		if cfg.ProxyHost != "192.168.1.100" {
			t.Fatalf("ProxyHost = %q, want %q", cfg.ProxyHost, "192.168.1.100")
		}
		if len(cfg.NoProxyIPRanges) != 2 {
			t.Fatalf("NoProxyIPRanges length = %d, want 2", len(cfg.NoProxyIPRanges))
		}
		if len(cfg.NoProxyDomains) != 2 {
			t.Fatalf("NoProxyDomains length = %d, want 2", len(cfg.NoProxyDomains))
		}
	})
}
