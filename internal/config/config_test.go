package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		setEnv    map[string]string
		wantErr   bool
		errMsg    string
		checkFunc func(*testing.T, Config)
	}{
		{
			name: "valid config with literal values",
			yaml: `
server:
  addr: ":8080"
privacy:
  mode: "strict"
  tenantSalt: "literal-salt-value"
sources:
  stripe:
    apiKey: "sk_test_123"
    account: "acct_123"
    maxRequestsPerSecond: 10
export:
  mode: "file"
  filePath: "/tmp/test.jsonl"
`,
			wantErr: false,
			checkFunc: func(t *testing.T, cfg Config) {
				if cfg.Server.Addr != ":8080" {
					t.Errorf("Server.Addr = %q, want %q", cfg.Server.Addr, ":8080")
				}
				if cfg.Privacy.Mode != "strict" {
					t.Errorf("Privacy.Mode = %q, want %q", cfg.Privacy.Mode, "strict")
				}
				if cfg.Privacy.TenantSalt != "literal-salt-value" {
					t.Errorf("Privacy.TenantSalt = %q, want %q", cfg.Privacy.TenantSalt, "literal-salt-value")
				}
				if cfg.Sources.Stripe == nil {
					t.Fatal("Sources.Stripe is nil")
				}
				if cfg.Sources.Stripe.APIKey != "sk_test_123" {
					t.Errorf("Sources.Stripe.APIKey = %q, want %q", cfg.Sources.Stripe.APIKey, "sk_test_123")
				}
				if cfg.Sources.Stripe.Account != "acct_123" {
					t.Errorf("Sources.Stripe.Account = %q, want %q", cfg.Sources.Stripe.Account, "acct_123")
				}
				if cfg.Sources.Stripe.MaxRequestsPerSecond != 10 {
					t.Errorf("Sources.Stripe.MaxRequestsPerSecond = %d, want %d", cfg.Sources.Stripe.MaxRequestsPerSecond, 10)
				}
			},
		},
		{
			name: "valid config with env indirections",
			yaml: `
server:
  addr: ":9000"
privacy:
  mode: "standard"
  tenantSalt: "env:TEST_TENANT_SALT"
sources:
  stripe:
    apiKey: "env:TEST_STRIPE_KEY"
    maxRequestsPerSecond: 5
export:
  mode: "file"
  filePath: "/tmp/test.jsonl"
`,
			setEnv: map[string]string{
				"TEST_TENANT_SALT": "env-resolved-salt",
				"TEST_STRIPE_KEY":  "sk_test_from_env",
			},
			wantErr: false,
			checkFunc: func(t *testing.T, cfg Config) {
				if cfg.Privacy.TenantSalt != "env-resolved-salt" {
					t.Errorf("Privacy.TenantSalt = %q, want %q", cfg.Privacy.TenantSalt, "env-resolved-salt")
				}
				if cfg.Sources.Stripe.APIKey != "sk_test_from_env" {
					t.Errorf("Sources.Stripe.APIKey = %q, want %q", cfg.Sources.Stripe.APIKey, "sk_test_from_env")
				}
			},
		},
		{
			name: "env indirection with whitespace",
			yaml: `
server:
  addr: ":8080"
privacy:
  mode: "strict"
  tenantSalt: "env: TEST_SALT_WHITESPACE "
sources:
  stripe:
    apiKey: "env: TEST_KEY_WHITESPACE "
    maxRequestsPerSecond: 8
export:
  mode: "file"
  filePath: "/tmp/test.jsonl"
`,
			setEnv: map[string]string{
				"TEST_SALT_WHITESPACE": "trimmed-salt",
				"TEST_KEY_WHITESPACE":  "trimmed-key",
			},
			wantErr: false,
			checkFunc: func(t *testing.T, cfg Config) {
				if cfg.Privacy.TenantSalt != "trimmed-salt" {
					t.Errorf("Privacy.TenantSalt = %q, want %q (env var should be trimmed)", cfg.Privacy.TenantSalt, "trimmed-salt")
				}
				if cfg.Sources.Stripe.APIKey != "trimmed-key" {
					t.Errorf("Sources.Stripe.APIKey = %q, want %q (env var should be trimmed)", cfg.Sources.Stripe.APIKey, "trimmed-key")
				}
			},
		},
		{
			name: "missing server.addr",
			yaml: `
server:
  addr: ""
privacy:
  mode: "strict"
  tenantSalt: "salt"
`,
			wantErr: true,
			errMsg:  "server.addr must be non-empty",
		},
		{
			name: "invalid privacy.mode",
			yaml: `
server:
  addr: ":8080"
privacy:
  mode: "invalid"
  tenantSalt: "salt"
`,
			wantErr: true,
			errMsg:  "privacy.mode must be 'strict' or 'standard'",
		},
		{
			name: "missing privacy.tenantSalt",
			yaml: `
server:
  addr: ":8080"
privacy:
  mode: "strict"
  tenantSalt: ""
`,
			wantErr: true,
			errMsg:  "privacy.tenantSalt must be set",
		},
		{
			name: "missing env variable for tenantSalt",
			yaml: `
server:
  addr: ":8080"
privacy:
  mode: "strict"
  tenantSalt: "env:NONEXISTENT_SALT"
`,
			wantErr: true,
			errMsg:  "privacy.tenantSalt must be set",
		},
		{
			name: "stripe present but missing apiKey",
			yaml: `
server:
  addr: ":8080"
privacy:
  mode: "strict"
  tenantSalt: "salt"
sources:
  stripe:
    apiKey: ""
    maxRequestsPerSecond: 10
`,
			wantErr: true,
			errMsg:  "sources.stripe.apiKey must be set",
		},
		{
			name: "stripe maxRequestsPerSecond defaulting (zero)",
			yaml: `
server:
  addr: ":8080"
privacy:
  mode: "strict"
  tenantSalt: "salt"
sources:
  stripe:
    apiKey: "sk_test_123"
    maxRequestsPerSecond: 0
export:
  mode: "file"
  filePath: "/tmp/test.jsonl"
`,
			wantErr: false,
			checkFunc: func(t *testing.T, cfg Config) {
				if cfg.Sources.Stripe.MaxRequestsPerSecond != 8 {
					t.Errorf("Sources.Stripe.MaxRequestsPerSecond = %d, want %d (default)", cfg.Sources.Stripe.MaxRequestsPerSecond, 8)
				}
			},
		},
		{
			name: "stripe maxRequestsPerSecond defaulting (negative)",
			yaml: `
server:
  addr: ":8080"
privacy:
  mode: "strict"
  tenantSalt: "salt"
sources:
  stripe:
    apiKey: "sk_test_123"
    maxRequestsPerSecond: -5
export:
  mode: "file"
  filePath: "/tmp/test.jsonl"
`,
			wantErr: false,
			checkFunc: func(t *testing.T, cfg Config) {
				if cfg.Sources.Stripe.MaxRequestsPerSecond != 8 {
					t.Errorf("Sources.Stripe.MaxRequestsPerSecond = %d, want %d (default)", cfg.Sources.Stripe.MaxRequestsPerSecond, 8)
				}
			},
		},
		{
			name: "no stripe source is valid",
			yaml: `
server:
  addr: ":8080"
privacy:
  mode: "strict"
  tenantSalt: "salt"
sources:
  stripe: null
export:
  mode: "file"
  filePath: "/tmp/test.jsonl"
`,
			wantErr: false,
			checkFunc: func(t *testing.T, cfg Config) {
				if cfg.Sources.Stripe != nil {
					t.Errorf("Sources.Stripe should be nil")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables for this test
			for k, v := range tt.setEnv {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			// Create a temporary config file
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")
			if err := os.WriteFile(configPath, []byte(tt.yaml), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			// Load the configuration
			cfg, err := Load(configPath)

			// Check error expectations
			if tt.wantErr {
				if err == nil {
					t.Errorf("Load() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("Load() error = %q, want %q", err.Error(), tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("Load() unexpected error = %v", err)
				return
			}

			// Run custom checks
			if tt.checkFunc != nil {
				tt.checkFunc(t, cfg)
			}
		})
	}
}

func TestLoadFileErrors(t *testing.T) {
	t.Run("nonexistent file", func(t *testing.T) {
		_, err := Load("/nonexistent/path/config.yaml")
		if err == nil {
			t.Error("Load() should return error for nonexistent file")
		}
	})

	t.Run("invalid YAML", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "invalid.yaml")
		invalidYAML := `
server:
  addr: ":8080"
  invalid_indent
privacy:
`
		if err := os.WriteFile(configPath, []byte(invalidYAML), 0644); err != nil {
			t.Fatalf("failed to write test config: %v", err)
		}

		_, err := Load(configPath)
		if err == nil {
			t.Error("Load() should return error for invalid YAML")
		}
	})
}

func TestExportConfigMigration(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		checkFunc func(*testing.T, Config)
	}{
		{
			name: "legacy config migrates to new structure",
			yaml: `
server:
  addr: ":8080"
privacy:
  mode: "strict"
  tenantSalt: "salt"
sources:
  stripe:
    apiKey: "sk_test_123"
export:
  mode: "file"
  endpoint: "https://api.example.com/ingest"
  filePath: "reports/legacy.jsonl"
`,
			checkFunc: func(t *testing.T, cfg Config) {
				// Verify legacy fields are migrated to new structure
				if cfg.Export.HTTP.BaseURL != "https://api.example.com/ingest" {
					t.Errorf("HTTP.BaseURL = %q, want %q", cfg.Export.HTTP.BaseURL, "https://api.example.com/ingest")
				}
				if cfg.Export.HTTP.Routes["edges"] != "/ingest/edges" {
					t.Errorf("HTTP.Routes[edges] = %q, want %q", cfg.Export.HTTP.Routes["edges"], "/ingest/edges")
				}
				if cfg.Export.File.Paths["edges"] != "reports/legacy.jsonl" {
					t.Errorf("File.Paths[edges] = %q, want %q", cfg.Export.File.Paths["edges"], "reports/legacy.jsonl")
				}
				// Verify defaults are applied
				if cfg.Export.HTTP.MaxRetries != 3 {
					t.Errorf("HTTP.MaxRetries = %d, want %d", cfg.Export.HTTP.MaxRetries, 3)
				}
				if cfg.Export.HTTP.BatchSize != 50 {
					t.Errorf("HTTP.BatchSize = %d, want %d", cfg.Export.HTTP.BatchSize, 50)
				}
			},
		},
		{
			name: "new per-stream config preserved",
			yaml: `
server:
  addr: ":8080"
privacy:
  mode: "strict"
  tenantSalt: "salt"
sources:
  stripe:
    apiKey: "sk_test_123"
export:
  mode: "both"
  http:
    baseUrl: "https://api.connektn.dev"
    routes:
      edges: "/v1/edges"
      billing: "/v1/billing"
    headers:
      authorizationEnv: "AUTH_TOKEN"
    maxRetries: 5
    batchSize: 100
    flushEvery: 10s
  file:
    paths:
      edges: "reports/edges.jsonl"
      billing: "reports/billing.jsonl"
`,
			checkFunc: func(t *testing.T, cfg Config) {
				if cfg.Export.HTTP.BaseURL != "https://api.connektn.dev" {
					t.Errorf("HTTP.BaseURL = %q, want %q", cfg.Export.HTTP.BaseURL, "https://api.connektn.dev")
				}
				if cfg.Export.HTTP.Routes["edges"] != "/v1/edges" {
					t.Errorf("HTTP.Routes[edges] = %q, want %q", cfg.Export.HTTP.Routes["edges"], "/v1/edges")
				}
				if cfg.Export.HTTP.Routes["billing"] != "/v1/billing" {
					t.Errorf("HTTP.Routes[billing] = %q, want %q", cfg.Export.HTTP.Routes["billing"], "/v1/billing")
				}
				if cfg.Export.HTTP.Headers.AuthorizationEnv != "AUTH_TOKEN" {
					t.Errorf("HTTP.Headers.AuthorizationEnv = %q, want %q", cfg.Export.HTTP.Headers.AuthorizationEnv, "AUTH_TOKEN")
				}
				if cfg.Export.HTTP.MaxRetries != 5 {
					t.Errorf("HTTP.MaxRetries = %d, want %d", cfg.Export.HTTP.MaxRetries, 5)
				}
				if cfg.Export.HTTP.BatchSize != 100 {
					t.Errorf("HTTP.BatchSize = %d, want %d", cfg.Export.HTTP.BatchSize, 100)
				}
				if cfg.Export.File.Paths["edges"] != "reports/edges.jsonl" {
					t.Errorf("File.Paths[edges] = %q, want %q", cfg.Export.File.Paths["edges"], "reports/edges.jsonl")
				}
				if cfg.Export.File.Paths["billing"] != "reports/billing.jsonl" {
					t.Errorf("File.Paths[billing] = %q, want %q", cfg.Export.File.Paths["billing"], "reports/billing.jsonl")
				}
			},
		},
		{
			name: "mode defaults to http",
			yaml: `
server:
  addr: ":8080"
privacy:
  mode: "strict"
  tenantSalt: "salt"
sources:
  stripe:
    apiKey: "sk_test_123"
export:
  http:
    baseUrl: "https://api.example.com"
  file:
    paths:
      edges: "reports/edges.jsonl"
`,
			checkFunc: func(t *testing.T, cfg Config) {
				if cfg.Export.Mode != "http" {
					t.Errorf("Export.Mode = %q, want %q (default)", cfg.Export.Mode, "http")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")
			if err := os.WriteFile(configPath, []byte(tt.yaml), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load() unexpected error = %v", err)
			}

			if tt.checkFunc != nil {
				tt.checkFunc(t, cfg)
			}
		})
	}
}

func TestExportValidation(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
		errMsg  string
	}{
		{
			name: "file mode requires edges path",
			yaml: `
server:
  addr: ":8080"
privacy:
  mode: "strict"
  tenantSalt: "salt"
sources:
  stripe:
    apiKey: "sk_test_123"
export:
  mode: "file"
  file:
    paths:
      billing: "reports/billing.jsonl"
`,
			wantErr: true,
			errMsg:  "export: file.paths.edges must be set when mode is 'file' or 'both'",
		},
		{
			name: "http mode requires baseUrl",
			yaml: `
server:
  addr: ":8080"
privacy:
  mode: "strict"
  tenantSalt: "salt"
sources:
  stripe:
    apiKey: "sk_test_123"
export:
  mode: "http"
  http:
    routes:
      edges: "/v1/edges"
`,
			wantErr: true,
			errMsg:  "export: http.baseUrl must be set when mode is 'http' or 'both'",
		},
		{
			name: "invalid mode rejected",
			yaml: `
server:
  addr: ":8080"
privacy:
  mode: "strict"
  tenantSalt: "salt"
sources:
  stripe:
    apiKey: "sk_test_123"
export:
  mode: "invalid"
`,
			wantErr: true,
			errMsg:  "export: mode must be 'http', 'file', or 'both'",
		},
		{
			name: "both mode requires both configs",
			yaml: `
server:
  addr: ":8080"
privacy:
  mode: "strict"
  tenantSalt: "salt"
sources:
  stripe:
    apiKey: "sk_test_123"
export:
  mode: "both"
  http:
    baseUrl: "https://api.example.com"
  file:
    paths:
      edges: "reports/edges.jsonl"
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")
			if err := os.WriteFile(configPath, []byte(tt.yaml), 0644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			_, err := Load(configPath)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Load() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("Load() error = %q, want %q", err.Error(), tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("Load() unexpected error = %v", err)
			}
		})
	}
}

func TestResolveEnvValue(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		setEnv map[string]string
		want   string
	}{
		{
			name:  "literal value unchanged",
			input: "literal-value",
			want:  "literal-value",
		},
		{
			name:  "env indirection resolved",
			input: "env:TEST_VAR",
			setEnv: map[string]string{
				"TEST_VAR": "resolved-value",
			},
			want: "resolved-value",
		},
		{
			name:   "env indirection with nonexistent var",
			input:  "env:NONEXISTENT",
			setEnv: map[string]string{},
			want:   "", // os.Getenv returns empty string
		},
		{
			name:  "env indirection with whitespace",
			input: "env: TEST_VAR_WHITESPACE ",
			setEnv: map[string]string{
				"TEST_VAR_WHITESPACE": "trimmed-value",
			},
			want: "trimmed-value",
		},
		{
			name:  "value starting with env but not indirection",
			input: "environment-variable",
			want:  "environment-variable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.setEnv {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			got := resolveEnvValue(tt.input)
			if got != tt.want {
				t.Errorf("resolveEnvValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
