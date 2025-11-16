// Package config provides a minimal, production-ready YAML configuration loader
// for the Connektn Linker Agent. It supports environment variable indirection
// via the "env:NAME" syntax and validates required fields.
//
// Security: This loader never logs or prints secret values. All string fields
// are treated as potentially sensitive.
package config

import (
	"fmt"
	"net"
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
	Mode                     string `yaml:"mode"`                     // "strict" or "standard" (legacy)
	IDMode                   string `yaml:"idMode"`                   // "strict" or "passthrough"
	TenantSalt               string `yaml:"tenantSalt"`               // may be "env:NAME"
	AllowPassthroughExports  bool   `yaml:"allowPassthroughExports"`  // allow HTTP export of raw IDs to non-Connektn endpoints
}

// StripeWebhookRetry configures retry behavior for webhook processing.
type StripeWebhookRetry struct {
	MaxAttempts int           `yaml:"maxAttempts"` // Max retry attempts (default: 5)
	BaseBackoff time.Duration `yaml:"baseBackoff"` // Base backoff duration (default: 2s)
	MaxBackoff  time.Duration `yaml:"maxBackoff"`  // Max backoff duration (default: 30s)
}

// StripeWebhook configures the Stripe webhook receiver.
type StripeWebhook struct {
	Enabled         bool               `yaml:"enabled"`         // Enable webhook receiver
	Path            string             `yaml:"path"`            // HTTP path (e.g., "/webhooks/stripe")
	SigningSecret   string             `yaml:"signingSecret"`   // may be "env:NAME"
	AllowedIPRanges []string           `yaml:"allowedIPRanges"` // Optional IP allowlist (CIDR notation)
	MaxSkew         time.Duration      `yaml:"maxSkew"`         // Timestamp tolerance (default: 300s)
	Retry           StripeWebhookRetry `yaml:"retry"`           // Retry configuration
}

// StripeSource configures the Stripe API connector.
type StripeSource struct {
	APIKey               string        `yaml:"apiKey"`               // may be "env:NAME"
	Account              string        `yaml:"account"`              // optional Stripe Connect account
	MaxRequestsPerSecond int           `yaml:"maxRequestsPerSecond"` // rate limit (default: 8)
	Webhook              StripeWebhook `yaml:"webhook"`              // Webhook configuration
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

// Heartbeat configures periodic heartbeat transmission to Connektn Cloud.
type Heartbeat struct {
	Enabled         bool          `yaml:"enabled"`         // Enable heartbeat transmission
	Interval        time.Duration `yaml:"interval"`        // Heartbeat interval (default: 30s)
	Endpoint        string        `yaml:"endpoint"`        // Cloud endpoint for heartbeats
	SignatureSecret string        `yaml:"signatureSecret"` // HMAC signature secret (may be "env:NAME")
}

// ControlNonceCache configures the nonce cache for replay protection.
type ControlNonceCache struct {
	Size int           `yaml:"size"` // Max number of nonces to cache (default: 1000)
	TTL  time.Duration `yaml:"ttl"`  // Nonce expiration time (default: 600s)
}

// Control configures the control command HTTP endpoint.
type Control struct {
	Enabled         bool              `yaml:"enabled"`         // Enable control command endpoint
	ListenAddr      string            `yaml:"listenAddr"`      // Listen address (default: ":8081")
	SignatureSecret string            `yaml:"signatureSecret"` // HMAC signature secret (may be "env:NAME")
	MaxClockSkew    time.Duration     `yaml:"maxClockSkew"`    // Max acceptable clock skew (default: 300s)
	NonceCache      ControlNonceCache `yaml:"nonceCache"`      // Nonce cache configuration
}

// Agent configures agent-specific settings.
type Agent struct {
	ID             string `yaml:"id"`             // Agent ID (generated on first run if empty)
	OrganizationID string `yaml:"organizationId"` // Organization ID (may be "env:NAME")
	Version        string `yaml:"version"`        // Agent version
}

// QueueStreamConfig configures a single queue stream
type QueueStreamConfig struct {
	MaxBytes    int64  `yaml:"maxBytes"`    // Maximum disk space (bytes)
	MaxEvents   int64  `yaml:"maxEvents"`   // Maximum number of events
	DropPolicy  string `yaml:"dropPolicy"`  // "drop_oldest" or "reject_new"
	SegmentSize int64  `yaml:"segmentSize"` // Segment size for custom WAL (not used in BadgerDB)
}

// QueueDLQConfig configures the dead letter queue
type QueueDLQConfig struct {
	MaxBytes      int64 `yaml:"maxBytes"`      // Maximum disk space (bytes)
	RetentionDays int   `yaml:"retentionDays"` // Auto-cleanup after N days
}

// QueueConfig configures durable queues
type QueueConfig struct {
	Type    string            `yaml:"type"`    // "memory" (legacy) or "durable" (new)
	WALPath string            `yaml:"walPath"` // Directory for BadgerDB data
	Billing QueueStreamConfig `yaml:"billing"` // Billing queue configuration
	Usage   QueueStreamConfig `yaml:"usage"`   // Usage queue configuration
	Edges   QueueStreamConfig `yaml:"edges"`   // Edges queue configuration
	DLQ     QueueDLQConfig    `yaml:"dlq"`     // Dead letter queue configuration
}

// Config is the root configuration structure.
type Config struct {
	Agent     Agent       `yaml:"agent"`
	Server    Server      `yaml:"server"`
	Privacy   Privacy     `yaml:"privacy"`
	Sources   Sources     `yaml:"sources"`
	Export    Export      `yaml:"export"`
	Matchers  Matchers    `yaml:"matchers"` // Matcher framework configuration
	Heartbeat Heartbeat   `yaml:"heartbeat"`
	Control   Control     `yaml:"control"`
	Queue     QueueConfig `yaml:"queue"` // Queue configuration
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
	cfg.Agent.OrganizationID = resolveEnvValue(cfg.Agent.OrganizationID)
	cfg.Heartbeat.SignatureSecret = resolveEnvValue(cfg.Heartbeat.SignatureSecret)
	cfg.Control.SignatureSecret = resolveEnvValue(cfg.Control.SignatureSecret)

	if cfg.Sources.Stripe != nil {
		cfg.Sources.Stripe.APIKey = resolveEnvValue(cfg.Sources.Stripe.APIKey)
		cfg.Sources.Stripe.Webhook.SigningSecret = resolveEnvValue(cfg.Sources.Stripe.Webhook.SigningSecret)
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

	// Default idMode to "strict" if not specified
	if cfg.Privacy.IDMode == "" {
		cfg.Privacy.IDMode = "strict"
	}

	// Validate idMode
	if cfg.Privacy.IDMode != "strict" && cfg.Privacy.IDMode != "passthrough" {
		return fmt.Errorf("privacy.idMode must be 'strict' or 'passthrough', got %q", cfg.Privacy.IDMode)
	}

	// tenantSalt is required in strict mode
	if cfg.Privacy.IDMode == "strict" && cfg.Privacy.TenantSalt == "" {
		return fmt.Errorf("privacy.tenantSalt must be set when idMode is 'strict'")
	}

	// Legacy: tenantSalt validation for existing configs
	if cfg.Privacy.TenantSalt == "" && cfg.Privacy.IDMode == "" {
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

		// Validate webhook configuration
		if err := validateStripeWebhook(&cfg.Sources.Stripe.Webhook); err != nil {
			return fmt.Errorf("sources.stripe.webhook: %w", err)
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

	// Apply heartbeat defaults and validate
	if err := validateHeartbeat(&cfg.Heartbeat); err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}

	// Apply control defaults and validate
	if err := validateControl(&cfg.Control); err != nil {
		return fmt.Errorf("control: %w", err)
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

// validateStripeWebhook validates and sets defaults for webhook configuration.
func validateStripeWebhook(webhook *StripeWebhook) error {
	// If webhook is not enabled, skip validation
	if !webhook.Enabled {
		return nil
	}

	// Validate required fields
	if webhook.SigningSecret == "" {
		return fmt.Errorf("signingSecret must be set when enabled is true")
	}

	if webhook.Path == "" {
		webhook.Path = "/webhooks/stripe"
	}

	if !strings.HasPrefix(webhook.Path, "/") {
		return fmt.Errorf("path must start with '/', got %q", webhook.Path)
	}

	// Set defaults
	if webhook.MaxSkew == 0 {
		webhook.MaxSkew = 300 * time.Second
	}

	if webhook.Retry.MaxAttempts == 0 {
		webhook.Retry.MaxAttempts = 5
	}

	if webhook.Retry.BaseBackoff == 0 {
		webhook.Retry.BaseBackoff = 2 * time.Second
	}

	if webhook.Retry.MaxBackoff == 0 {
		webhook.Retry.MaxBackoff = 30 * time.Second
	}

	// Validate allowed IP ranges if provided
	if len(webhook.AllowedIPRanges) > 0 {
		for _, cidr := range webhook.AllowedIPRanges {
			if _, _, err := net.ParseCIDR(cidr); err != nil {
				return fmt.Errorf("invalid CIDR notation in allowedIPRanges: %q", cidr)
			}
		}
	}

	return nil
}

// validateHeartbeat validates and applies defaults to heartbeat configuration
func validateHeartbeat(hb *Heartbeat) error {
	// Apply defaults
	if hb.Interval == 0 {
		hb.Interval = 30 * time.Second
	}

	// If heartbeat is enabled, validate required fields
	if hb.Enabled {
		if hb.Endpoint == "" {
			return fmt.Errorf("endpoint must be set when heartbeat is enabled")
		}
		if hb.SignatureSecret == "" {
			return fmt.Errorf("signatureSecret must be set when heartbeat is enabled")
		}
	}

	return nil
}

// validateControl validates and applies defaults to control configuration
func validateControl(ctrl *Control) error {
	// Apply defaults
	if ctrl.ListenAddr == "" {
		ctrl.ListenAddr = ":8081"
	}
	if ctrl.MaxClockSkew == 0 {
		ctrl.MaxClockSkew = 300 * time.Second // 5 minutes
	}
	if ctrl.NonceCache.Size == 0 {
		ctrl.NonceCache.Size = 1000
	}
	if ctrl.NonceCache.TTL == 0 {
		ctrl.NonceCache.TTL = 600 * time.Second // 10 minutes
	}

	// If control is enabled, validate required fields
	if ctrl.Enabled {
		if ctrl.SignatureSecret == "" {
			return fmt.Errorf("signatureSecret must be set when control is enabled")
		}
	}

	return nil
}
