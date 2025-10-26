// Package matchers implements the intelligence layer that links sanitized usage events
// with sanitized billing data (subscriptions/invoices) and emits explainable link edges
// with confidence scores and cryptographic proofs.
//
// Privacy: All identifiers are synthetic (HMAC-derived). No PII is ever logged or exported.
//
// Threat model: Matchers operate on already-sanitized data with synthetic IDs.
// LinkEdge proofs are cryptographically verifiable but contain no reversible customer data.
package matchers

import (
	"fmt"
)

// UsageEvent represents a sanitized, PII-free usage event from analytics/product logs.
// All identifiers are synthetic (HMAC-derived with tenant salt).
type UsageEvent struct {
	User    string // Synthetic user ID (e.g., syn_user_abc123...)
	Feature string // Feature name (e.g., "export_csv", "api_call")
	SKU     string // SKU/product identifier (e.g., "addon_export_pro")
	At      int64  // Unix timestamp in seconds
}

// LinkEdge is the output of the matching process: an explainable, exportable,
// PII-free edge connecting two synthetic nodes with confidence and proof.
//
// Privacy: All IDs are synthetic. Proof is a cryptographic hash. Notes contain
// only non-sensitive technical explanations (no PII, no raw IDs).
type LinkEdge struct {
	From       string  // Source node ID (e.g., syn_user_xxx, syn_sess_xxx)
	To         string  // Target node ID (e.g., syn_sub_xxx, syn_inv_xxx, syn_price_xxx)
	Kind       string  // Edge type (e.g., "user->subscription", "user->invoice")
	Confidence float64 // Confidence score in range [0, 1]
	Proof      string  // Cryptographic proof hex string (HMAC over inputs)
	Recipe     string  // Recipe name/version used for matching (e.g., "default/v1")
	At         int64   // Event timestamp or decision timestamp (Unix seconds)
	Notes      string  // Short natural-language rationale (no PII, max ~100 chars)
}

// Matcher is the interface implemented by all matching algorithms.
// Each matcher examines usage events and billing data to produce link edges.
type Matcher interface {
	// Name returns the unique identifier for this matcher (e.g., "deterministic_id").
	Name() string

	// Match processes usage events and billing data to produce link edges.
	// Returns a slice of LinkEdges with confidence scores and proofs.
	Match(usages []UsageEvent, subs []SubscriptionLite, invs []InvoiceLite) ([]LinkEdge, error)
}

// SubscriptionLite is a sanitized, lightweight representation of a subscription
// containing only synthetic IDs and metadata needed for matching.
//
// Privacy: All IDs are synthetic (HMAC-derived). No customer PII.
type SubscriptionLite struct {
	ID         string   // Synthetic subscription ID (syn_sub_xxx)
	Customer   string   // Synthetic customer ID (syn_cust_xxx)
	PriceIDs   []string // Synthetic price IDs (syn_price_xxx)
	ProductIDs []string // Synthetic product IDs (syn_prod_xxx)
	CreatedAt  int64    // Unix timestamp in seconds
	Status     string   // Subscription status (active, trialing, etc.)
}

// InvoiceLite is a sanitized, lightweight representation of an invoice
// containing only synthetic IDs and metadata needed for matching.
//
// Privacy: All IDs are synthetic (HMAC-derived). No customer PII.
type InvoiceLite struct {
	ID           string // Synthetic invoice ID (syn_inv_xxx)
	Customer     string // Synthetic customer ID (syn_cust_xxx)
	Subscription string // Synthetic subscription ID (syn_sub_xxx, empty if not linked)
	Total        int64  // Amount in currency minor units (e.g., cents)
	Currency     string // Three-letter currency code (e.g., "usd")
	Status       string // Invoice status (draft, open, paid, void, uncollectible)
	CreatedAt    int64  // Unix timestamp in seconds
	Paid         bool   // Whether invoice has been paid
}

// Recipe defines the configuration for the ensemble matcher, including
// weights for each matcher, acceptance threshold, and matcher-specific parameters.
type Recipe struct {
	Name              string             // Recipe name (e.g., "default")
	Version           string             // Recipe version (e.g., "v1")
	Weights           map[string]float64 // Weights per matcher name (e.g., {"deterministic_id": 1.0})
	Threshold         float64            // Minimum combined score to accept an edge (e.g., 0.8)
	TemporalWindowSec int64              // Time window in seconds for temporal matcher (e.g., 3600)
	SKUOverlapMin     float64            // Minimum overlap score for SKU matcher (e.g., 0.5)
}

// Within checks if a timestamp ts is within windowSec seconds of a center timestamp.
// Returns true if |ts - center| <= windowSec, false otherwise.
//
// Example: Within(1000, 1030, 60) returns true (30s difference < 60s window)
func Within(ts, center, windowSec int64) bool {
	if windowSec <= 0 {
		return false
	}
	diff := ts - center
	if diff < 0 {
		diff = -diff
	}
	return diff <= windowSec
}

// Validate checks if the Recipe configuration is valid.
func (r Recipe) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("recipe name must not be empty")
	}
	if r.Version == "" {
		return fmt.Errorf("recipe version must not be empty")
	}
	if r.Threshold < 0 || r.Threshold > 1 {
		return fmt.Errorf("recipe threshold must be in range [0, 1], got %.2f", r.Threshold)
	}
	if r.TemporalWindowSec < 0 {
		return fmt.Errorf("temporal window must be non-negative, got %d", r.TemporalWindowSec)
	}
	if r.SKUOverlapMin < 0 || r.SKUOverlapMin > 1 {
		return fmt.Errorf("SKU overlap min must be in range [0, 1], got %.2f", r.SKUOverlapMin)
	}
	return nil
}
