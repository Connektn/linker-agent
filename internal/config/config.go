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

// Config is the root configuration structure.
type Config struct {
	Server  Server  `yaml:"server"`
	Privacy Privacy `yaml:"privacy"`
	Sources Sources `yaml:"sources"`
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

	return nil
}
