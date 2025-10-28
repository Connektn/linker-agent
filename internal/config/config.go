// Package config provides a minimal, production-ready YAML configuration loader
// for the Connektn Linker Agent. It supports environment variable indirection
// via the "env:NAME" syntax and validates required fields.
//
// Security: This loader never logs or prints secret values. All string fields
// are treated as potentially sensitive.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Server holds HTTP server configuration.
type Server struct {
	Addr string `yaml:"addr"`
}

// Privacy controls privacy-preserving behavior and synthetic ID generation.
type Privacy struct {
	Mode       string `yaml:"mode"`       // "strict" or "standard"
	TenantSalt string `yaml:"tenantSalt"` // may be "env:NAME"
}

// StripeSource configures the Stripe API connector.
type StripeSource struct {
	APIKey               string `yaml:"apiKey"`               // may be "env:NAME"
	Account              string `yaml:"account"`              // optional Stripe Connect account
	MaxRequestsPerSecond int    `yaml:"maxRequestsPerSecond"` // rate limit (default: 8)
}

// Sources aggregates all data source connectors.
type Sources struct {
	Stripe *StripeSource `yaml:"stripe"`
}

// ExportHTTPHeaders configures HTTP header behavior.
type ExportHTTPHeaders struct {
	AuthorizationEnv string `yaml:"authorizationEnv"` // env var name for Bearer token
}

// ExportHTTP configures HTTP export with per-stream routing.
type ExportHTTP struct {
	BaseURL    string            `yaml:"baseUrl"`    // Base URL (e.g., "https://api.connektn.io")
	Routes     map[string]string `yaml:"routes"`     // Per-stream routes (e.g., "edges": "/ingest/edges")
	Headers    ExportHTTPHeaders `yaml:"headers"`    // HTTP headers configuration
	MaxRetries int               `yaml:"maxRetries"` // Max retry attempts (default: 3)
	BatchSize  int               `yaml:"batchSize"`  // Batch size for HTTP requests (default: 50)
	FlushEvery time.Duration     `yaml:"flushEvery"` // Flush interval (default: 5s)
}

// ExportFile configures file export with per-stream paths.
type ExportFile struct {
	Paths map[string]string `yaml:"paths"` // Per-stream file paths (e.g., "edges": "reports/link_edges.jsonl")
}

// Export configures export sink behavior (HTTP endpoint and/or local file).
// Supports both new per-stream configuration and legacy single-sink configuration.
type Export struct {
	Mode     string     `yaml:"mode"`     // "http" | "file" | "both" (default: "http")
	Endpoint string     `yaml:"endpoint"` // Legacy: HTTP endpoint URL
	FilePath string     `yaml:"filePath"` // Legacy: Local file path
	HTTP     ExportHTTP `yaml:"http"`     // New: per-stream HTTP configuration
	File     ExportFile `yaml:"file"`     // New: per-stream file configuration
}

// MatcherRecipe defines the configuration for the ensemble matcher.
// This includes weights for each matcher, acceptance threshold, and matcher-specific parameters.
type MatcherRecipe struct {
	Name              string             `yaml:"name"`              // Recipe name (e.g., "default")
	Version           string             `yaml:"version"`           // Recipe version (e.g., "v1")
	Weights           map[string]float64 `yaml:"weights"`           // Weights per matcher name
	Threshold         float64            `yaml:"threshold"`         // Minimum combined score to accept an edge
	TemporalWindowSec int64              `yaml:"temporalWindowSec"` // Time window in seconds for temporal matcher
	SKUOverlapMin     float64            `yaml:"skuOverlapMin"`     // Minimum overlap score for SKU matcher
}

// Matchers configures the matcher framework behavior.
type Matchers struct {
	Recipe MatcherRecipe `yaml:"recipe"` // Ensemble recipe configuration
}

// Config is the root configuration structure.
type Config struct {
	Server   Server   `yaml:"server"`
	Privacy  Privacy  `yaml:"privacy"`
	Sources  Sources  `yaml:"sources"`
	Export   Export   `yaml:"export"`
	Matchers Matchers `yaml:"matchers"` // Matcher framework configuration
}

// Load reads a YAML configuration file from the given path, resolves environment
// variable indirections (env:NAME), validates required fields, and returns a
// typed Config.
//
// Returns an error if the file cannot be read, YAML is invalid, or validation fails.
// No secrets or PII are included in error messages.
func Load(path string) (Config, error) {
	var cfg Config

	// Read the configuration file
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("failed to read config file: %w", err)
	}

	// Unmarshal YAML into the Config struct
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Resolve environment variable indirections
	resolveEnv(&cfg)

	// Validate the configuration
	if err := validate(&cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// resolveEnv walks through the config and replaces any string value in the form
// "env:NAME" with the value from os.Getenv("NAME"). Whitespace around NAME is trimmed.
func resolveEnv(cfg *Config) {
	cfg.Privacy.TenantSalt = resolveEnvValue(cfg.Privacy.TenantSalt)

	if cfg.Sources.Stripe != nil {
		cfg.Sources.Stripe.APIKey = resolveEnvValue(cfg.Sources.Stripe.APIKey)
	}
}

// resolveEnvValue resolves a single value that may be in the form "env:NAME".
// If the value starts with "env:", it extracts NAME (trimmed) and returns os.Getenv(NAME).
// Otherwise, returns the value unchanged.
func resolveEnvValue(val string) string {
	const prefix = "env:"
	if !strings.HasPrefix(val, prefix) {
		return val
	}

	// Extract the environment variable name and trim whitespace
	envName := strings.TrimSpace(strings.TrimPrefix(val, prefix))
	return os.Getenv(envName)
}

// validate checks that all required fields are present and have valid values.
// Returns a descriptive error if validation fails. Error messages are generic
// and do not echo secret values.
func validate(cfg *Config) error {
	// Validate server configuration
	if cfg.Server.Addr == "" {
		return fmt.Errorf("server.addr must be non-empty")
	}

	// Validate privacy configuration
	if cfg.Privacy.Mode != "strict" && cfg.Privacy.Mode != "standard" {
		return fmt.Errorf("privacy.mode must be 'strict' or 'standard'")
	}

	if cfg.Privacy.TenantSalt == "" {
		return fmt.Errorf("privacy.tenantSalt must be set")
	}

	// Validate Stripe source if present
	if cfg.Sources.Stripe != nil {
		if cfg.Sources.Stripe.APIKey == "" {
			return fmt.Errorf("sources.stripe.apiKey must be set")
		}

		// Default maxRequestsPerSecond to 8 if not positive
		if cfg.Sources.Stripe.MaxRequestsPerSecond <= 0 {
			cfg.Sources.Stripe.MaxRequestsPerSecond = 8
		}
	}

	// Validate and migrate export configuration
	if err := validateAndMigrateExport(&cfg.Export); err != nil {
		return fmt.Errorf("export: %w", err)
	}

	// Validate matcher configuration
	if err := validateMatcherRecipe(&cfg.Matchers.Recipe); err != nil {
		return fmt.Errorf("matchers.recipe: %w", err)
	}

	return nil
}

// validateAndMigrateExport validates and migrates export configuration.
// Legacy fields (Endpoint, FilePath) are migrated to new per-stream configuration.
func validateAndMigrateExport(export *Export) error {
	// Default mode to "http" if not specified
	if export.Mode == "" {
		export.Mode = "http"
	}

	// Validate mode value
	if export.Mode != "http" && export.Mode != "file" && export.Mode != "both" {
		return fmt.Errorf("mode must be 'http', 'file', or 'both'")
	}

	// Migrate legacy configuration to new per-stream configuration
	migrateLegacyExportConfig(export)

	// Apply defaults for HTTP configuration
	if export.HTTP.MaxRetries == 0 {
		export.HTTP.MaxRetries = 3
	}
	if export.HTTP.BatchSize == 0 {
		export.HTTP.BatchSize = 50
	}
	if export.HTTP.FlushEvery == 0 {
		export.HTTP.FlushEvery = 5 * time.Second
	}

	// Validate HTTP configuration if mode includes "http"
	if export.Mode == "http" || export.Mode == "both" {
		if export.HTTP.BaseURL == "" {
			return fmt.Errorf("http.baseUrl must be set when mode is 'http' or 'both'")
		}
		// Ensure edges route exists (required stream)
		if export.HTTP.Routes == nil {
			export.HTTP.Routes = make(map[string]string)
		}
		if export.HTTP.Routes["edges"] == "" {
			export.HTTP.Routes["edges"] = "/ingest/edges"
		}
	}

	// Validate file configuration if mode includes "file"
	if export.Mode == "file" || export.Mode == "both" {
		// Ensure edges path exists (required stream)
		if export.File.Paths == nil {
			export.File.Paths = make(map[string]string)
		}
		if export.File.Paths["edges"] == "" {
			return fmt.Errorf("file.paths.edges must be set when mode is 'file' or 'both'")
		}
	}

	return nil
}

// migrateLegacyExportConfig migrates legacy export configuration to new per-stream format.
// If legacy fields (Endpoint, FilePath) are set and new fields are empty, it copies them over.
func migrateLegacyExportConfig(export *Export) {
	// Migrate legacy Endpoint to HTTP.BaseURL
	if export.Endpoint != "" && export.HTTP.BaseURL == "" {
		export.HTTP.BaseURL = export.Endpoint
	}

	// Migrate legacy FilePath to File.Paths["edges"]
	if export.FilePath != "" {
		if export.File.Paths == nil {
			export.File.Paths = make(map[string]string)
		}
		if export.File.Paths["edges"] == "" {
			export.File.Paths["edges"] = export.FilePath
		}
	}

	// Ensure default routes if using HTTP
	if export.HTTP.Routes == nil && export.HTTP.BaseURL != "" {
		export.HTTP.Routes = make(map[string]string)
	}
	// Set default edges route if not specified
	if export.HTTP.BaseURL != "" && export.HTTP.Routes["edges"] == "" {
		export.HTTP.Routes["edges"] = "/ingest/edges"
	}
}

// validateMatcherRecipe validates the matcher recipe configuration.
func validateMatcherRecipe(recipe *MatcherRecipe) error {
	// Set defaults if not specified
	if recipe.Name == "" {
		recipe.Name = "default"
	}
	if recipe.Version == "" {
		recipe.Version = "v1"
	}

	// Default weights if not specified
	if recipe.Weights == nil {
		recipe.Weights = map[string]float64{
			"deterministic_id":   1.0,
			"temporal_proximity": 0.5,
			"sku_overlap":        0.8,
		}
	}

	// Default threshold
	if recipe.Threshold == 0 {
		recipe.Threshold = 0.8
	}

	// Default temporal window (1 hour)
	if recipe.TemporalWindowSec == 0 {
		recipe.TemporalWindowSec = 3600
	}

	// Default SKU overlap min
	if recipe.SKUOverlapMin == 0 {
		recipe.SKUOverlapMin = 0.5
	}

	// Validate threshold range
	if recipe.Threshold < 0 || recipe.Threshold > 1 {
		return fmt.Errorf("threshold must be in range [0, 1], got %.2f", recipe.Threshold)
	}

	// Validate temporal window
	if recipe.TemporalWindowSec < 0 {
		return fmt.Errorf("temporalWindowSec must be non-negative, got %d", recipe.TemporalWindowSec)
	}

	// Validate SKU overlap min
	if recipe.SKUOverlapMin < 0 || recipe.SKUOverlapMin > 1 {
		return fmt.Errorf("skuOverlapMin must be in range [0, 1], got %.2f", recipe.SKUOverlapMin)
	}

	// Validate weights (all must be non-negative)
	for matcher, weight := range recipe.Weights {
		if weight < 0 {
			return fmt.Errorf("weight for matcher '%s' must be non-negative, got %.2f", matcher, weight)
		}
	}

	return nil
}
