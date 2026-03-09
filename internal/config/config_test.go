package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		yaml       string
		wantErr    string
		assertions func(t *testing.T, cfg *Config)
	}{
		{
			name: "valid config applies defaults and normalization",
			yaml: `server:
  host: 127.0.0.1
  port: 9090
whitelist:
  enabled: true
  domains:
    - EXAMPLE.COM
    - Api.Service.Local
logging:
  level: DEBUG
  format: TEXT
auth:
  enabled: true
  credentials:
    - username: alice
      password: secret
`,
			assertions: func(t *testing.T, cfg *Config) {
				t.Helper()

				if cfg.Server.Host != "127.0.0.1" {
					t.Fatalf("Host = %q, want %q", cfg.Server.Host, "127.0.0.1")
				}
				if cfg.Server.Port != 9090 {
					t.Fatalf("Port = %d, want %d", cfg.Server.Port, 9090)
				}
				if cfg.Server.ReadTimeout != defaultReadTimeout {
					t.Fatalf("ReadTimeout = %d, want %d", cfg.Server.ReadTimeout, defaultReadTimeout)
				}
				if cfg.Server.WriteTimeout != defaultWriteTimeout {
					t.Fatalf("WriteTimeout = %d, want %d", cfg.Server.WriteTimeout, defaultWriteTimeout)
				}
				if cfg.Server.IdleTimeout != defaultIdleTimeout {
					t.Fatalf("IdleTimeout = %d, want %d", cfg.Server.IdleTimeout, defaultIdleTimeout)
				}
				if cfg.Logging.Level != "debug" {
					t.Fatalf("Level = %q, want %q", cfg.Logging.Level, "debug")
				}
				if cfg.Logging.Format != "text" {
					t.Fatalf("Format = %q, want %q", cfg.Logging.Format, "text")
				}
				if cfg.Logging.AccessLog != defaultAccessLog {
					t.Fatalf("AccessLog = %q, want %q", cfg.Logging.AccessLog, defaultAccessLog)
				}
				wantDomains := []string{"example.com", "api.service.local"}
				for i, want := range wantDomains {
					if cfg.Whitelist.Domains[i] != want {
						t.Fatalf("Domains[%d] = %q, want %q", i, cfg.Whitelist.Domains[i], want)
					}
				}
				if got := cfg.Address(); got != "127.0.0.1:9090" {
					t.Fatalf("Address() = %q, want %q", got, "127.0.0.1:9090")
				}
			},
		},
		{
			name: "missing fields use defaults",
			yaml: `whitelist:
  enabled: false
auth:
  enabled: false
`,
			assertions: func(t *testing.T, cfg *Config) {
				t.Helper()

				if cfg.Server.Host != defaultHost {
					t.Fatalf("Host = %q, want %q", cfg.Server.Host, defaultHost)
				}
				if cfg.Server.Port != defaultPort {
					t.Fatalf("Port = %d, want %d", cfg.Server.Port, defaultPort)
				}
				if cfg.Logging.Level != defaultLogLevel {
					t.Fatalf("Level = %q, want %q", cfg.Logging.Level, defaultLogLevel)
				}
				if cfg.Logging.Format != defaultLogFormat {
					t.Fatalf("Format = %q, want %q", cfg.Logging.Format, defaultLogFormat)
				}
				if cfg.Logging.AccessLog != defaultAccessLog {
					t.Fatalf("AccessLog = %q, want %q", cfg.Logging.AccessLog, defaultAccessLog)
				}
				if got := cfg.Address(); got != "0.0.0.0:8080" {
					t.Fatalf("Address() = %q, want %q", got, "0.0.0.0:8080")
				}
			},
		},
		{
			name: "bad port",
			yaml: `server:
  port: 70000
`,
			wantErr: "server.port must be between 1 and 65535",
		},
		{
			name: "empty whitelist when enabled",
			yaml: `whitelist:
  enabled: true
  domains: []
`,
			wantErr: "whitelist.domains must not be empty when whitelist is enabled",
		},
		{
			name: "malformed auth credentials missing password",
			yaml: `auth:
  enabled: true
  credentials:
    - username: alice
`,
			wantErr: "auth.credentials entries must include username and password",
		},
		{
			name: "auth enabled without credentials",
			yaml: `auth:
  enabled: true
  credentials: []
`,
			wantErr: "auth.credentials must not be empty when auth is enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			cfg, err := Load(path)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Load() error = nil, want %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Load() error = %q, want substring %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if tt.assertions != nil {
				tt.assertions(t, cfg)
			}
		})
	}
}
