package matchers

import (
	"fmt"
	"linker-agent/internal/crypto"
	"strconv"
)

// TemporalMatcher correlates usage events with billing events based on temporal proximity.
// If a usage event occurs near (within a time window) a subscription creation or invoice
// generation, it suggests a relationship.
//
// Matching logic:
// - If usage timestamp is within ±WindowSec of subscription.CreatedAt → user->subscription edge
// - If usage timestamp is within ±WindowSec of invoice.CreatedAt → user->invoice edge
// - Confidence is proportional to temporal proximity (closer = higher confidence)
//
// Privacy: All IDs are synthetic. Proofs include time difference but no PII.
type TemporalMatcher struct {
	WindowSec  int64  // Time window in seconds (±)
	TenantSalt []byte // Used for generating cryptographic proofs
}

// Name returns the unique identifier for this matcher.
func (m TemporalMatcher) Name() string {
	return "temporal_proximity"
}

// Match performs temporal proximity matching between usage events and billing data.
// Returns link edges with confidence scores based on temporal proximity.
func (m TemporalMatcher) Match(usages []UsageEvent, subs []SubscriptionLite, invs []InvoiceLite) ([]LinkEdge, error) {
	var edges []LinkEdge

	// Process each usage event
	for _, usage := range usages {
		// Match usage -> subscriptions (temporal proximity)
		for _, sub := range subs {
			if Within(usage.At, sub.CreatedAt, m.WindowSec) {
				confidence := m.calculateConfidence(usage.At, sub.CreatedAt)
				diffSec := usage.At - sub.CreatedAt
				if diffSec < 0 {
					diffSec = -diffSec
				}

				proof, err := m.generateProof(usage.User, sub.ID, "user->subscription", diffSec)
				if err != nil {
					return nil, fmt.Errorf("failed to generate proof: %w", err)
				}

				notes := fmt.Sprintf("usage within %ds of subscription creation", diffSec)
				edges = append(edges, LinkEdge{
					From:       usage.User,
					To:         sub.ID,
					Kind:       "user->subscription",
					Confidence: confidence,
					Proof:      proof,
					Recipe:     "temporal_proximity/v1",
					At:         usage.At,
					Notes:      notes,
				})
			}
		}

		// Match usage -> invoices (temporal proximity)
		for _, inv := range invs {
			if Within(usage.At, inv.CreatedAt, m.WindowSec) {
				confidence := m.calculateConfidence(usage.At, inv.CreatedAt)
				diffSec := usage.At - inv.CreatedAt
				if diffSec < 0 {
					diffSec = -diffSec
				}

				proof, err := m.generateProof(usage.User, inv.ID, "user->invoice", diffSec)
				if err != nil {
					return nil, fmt.Errorf("failed to generate invoice proof: %w", err)
				}

				notes := fmt.Sprintf("usage within %ds of invoice creation", diffSec)
				edges = append(edges, LinkEdge{
					From:       usage.User,
					To:         inv.ID,
					Kind:       "user->invoice",
					Confidence: confidence,
					Proof:      proof,
					Recipe:     "temporal_proximity/v1",
					At:         usage.At,
					Notes:      notes,
				})
			}
		}
	}

	return edges, nil
}

// calculateConfidence computes a confidence score based on temporal proximity.
// Closer events receive higher confidence scores (up to 0.6 for exact match).
// The confidence decreases linearly as the time difference approaches the window boundary.
//
// Formula: confidence = 0.6 * (1 - |diff| / window)
// Range: (0, 0.6] (never 1.0 since this is probabilistic, not deterministic)
func (m TemporalMatcher) calculateConfidence(ts1, ts2 int64) float64 {
	if m.WindowSec == 0 {
		return 0.0
	}

	diff := ts1 - ts2
	if diff < 0 {
		diff = -diff
	}

	// Linear decay from 0.6 (exact match) to 0 (window boundary)
	// Base confidence is 0.6 to distinguish from deterministic (1.0)
	baseConfidence := 0.6
	decay := float64(diff) / float64(m.WindowSec)
	confidence := baseConfidence * (1.0 - decay)

	// Ensure confidence stays in valid range
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 0.6 {
		confidence = 0.6
	}

	return confidence
}

// generateProof creates a cryptographic proof (HMAC-SHA256) for a temporal link edge.
// The proof includes the time difference to make it verifiable.
//
// Privacy: All inputs are synthetic IDs or time differences (no PII).
func (m TemporalMatcher) generateProof(from, to, kind string, diffSec int64) (string, error) {
	// Concatenate inputs for proof generation
	input := fmt.Sprintf("%s|%s|%s|%s|%s", from, to, kind, m.Name(), strconv.FormatInt(diffSec, 10))
	proof, err := crypto.HMACSHA256Hex(m.TenantSalt, input)
	if err != nil {
		return "", err
	}
	return proof, nil
}
