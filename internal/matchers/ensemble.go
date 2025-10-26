package matchers

import (
	"fmt"
	"linker-agent/internal/crypto"
	"sort"
	"strings"
)

// Ensemble combines evidence from multiple matchers using weighted scoring
// with a threshold-based acceptance policy.
//
// Privacy: All IDs are synthetic. Proofs are cryptographic hashes over synthetic inputs.
type Ensemble struct {
	Recipe   Recipe
	Matchers []Matcher
	// TenantSalt is used for generating combined proofs
	TenantSalt []byte
}

// CandidateKey uniquely identifies a potential link edge by its endpoints and type.
type CandidateKey struct {
	From string // Source node ID (e.g., syn_user_xxx)
	To   string // Target node ID (e.g., syn_sub_xxx, syn_inv_xxx)
	Kind string // Edge type (e.g., "user->subscription")
}

// candidateEvidence accumulates evidence from multiple matchers for a single candidate link.
type candidateEvidence struct {
	Key            CandidateKey
	Edges          []LinkEdge  // All edges from different matchers
	CombinedScore  float64     // Weighted sum of confidences
	BestConfidence float64     // Highest individual confidence
	Timestamp      int64       // Latest timestamp
	Proofs         []string    // All contributing proofs
	Notes          []string    // All contributing notes
	Recipes        []string    // All contributing recipes
}

// Match runs all matchers and returns their raw edges (before ensemble combination).
// This is useful for debugging or when you want to see individual matcher outputs.
func (e Ensemble) Match(usages []UsageEvent, subs []SubscriptionLite, invs []InvoiceLite) ([]LinkEdge, error) {
	var allEdges []LinkEdge

	for _, matcher := range e.Matchers {
		edges, err := matcher.Match(usages, subs, invs)
		if err != nil {
			return nil, fmt.Errorf("matcher %s failed: %w", matcher.Name(), err)
		}
		allEdges = append(allEdges, edges...)
	}

	return allEdges, nil
}

// Combine aggregates edges from multiple matchers using weighted scoring and threshold filtering.
//
// Algorithm:
// 1. Group edges by (From, To, Kind)
// 2. For each group, compute weighted sum: Σ(weight[matcher] * confidence)
// 3. If combined score >= Recipe.Threshold, emit a single consolidated edge
// 4. Final confidence = min(1.0, combinedScore)
// 5. Proof = HMAC over all contributing proofs
// 6. Notes = concatenation of all notes (max 200 chars)
//
// Privacy: All inputs are synthetic IDs. Output proofs contain no PII.
func (e Ensemble) Combine(allEdges []LinkEdge) []LinkEdge {
	if len(allEdges) == 0 {
		return nil
	}

	// Group edges by candidate key
	candidates := make(map[CandidateKey]*candidateEvidence)

	for _, edge := range allEdges {
		key := CandidateKey{
			From: edge.From,
			To:   edge.To,
			Kind: edge.Kind,
		}

		if _, exists := candidates[key]; !exists {
			candidates[key] = &candidateEvidence{
				Key:       key,
				Edges:     []LinkEdge{},
				Timestamp: edge.At,
			}
		}

		candidate := candidates[key]
		candidate.Edges = append(candidate.Edges, edge)
		candidate.Proofs = append(candidate.Proofs, edge.Proof)
		candidate.Notes = append(candidate.Notes, edge.Notes)
		candidate.Recipes = append(candidate.Recipes, edge.Recipe)

		// Track highest confidence
		if edge.Confidence > candidate.BestConfidence {
			candidate.BestConfidence = edge.Confidence
		}

		// Track latest timestamp
		if edge.At > candidate.Timestamp {
			candidate.Timestamp = edge.At
		}
	}

	// Compute weighted scores and filter by threshold
	var accepted []LinkEdge

	for _, candidate := range candidates {
		// Calculate weighted sum
		combinedScore := e.calculateWeightedScore(candidate.Edges)
		candidate.CombinedScore = combinedScore

		// Apply threshold
		if combinedScore < e.Recipe.Threshold {
			continue
		}

		// Cap confidence at 1.0
		finalConfidence := combinedScore
		if finalConfidence > 1.0 {
			finalConfidence = 1.0
		}

		// Generate combined proof
		proof, err := e.generateCombinedProof(candidate)
		if err != nil {
			// Fallback: use the highest-confidence proof
			proof = e.selectBestProof(candidate.Edges)
		}

		// Consolidate notes (short, no PII)
		notes := e.consolidateNotes(candidate.Notes, combinedScore)

		// Create consolidated edge
		edge := LinkEdge{
			From:       candidate.Key.From,
			To:         candidate.Key.To,
			Kind:       candidate.Key.Kind,
			Confidence: finalConfidence,
			Proof:      proof,
			Recipe:     fmt.Sprintf("%s/%s", e.Recipe.Name, e.Recipe.Version),
			At:         candidate.Timestamp,
			Notes:      notes,
		}

		accepted = append(accepted, edge)
	}

	// Sort by confidence (descending) for deterministic output order
	sort.Slice(accepted, func(i, j int) bool {
		if accepted[i].Confidence != accepted[j].Confidence {
			return accepted[i].Confidence > accepted[j].Confidence
		}
		// Tie-break by From ID
		return accepted[i].From < accepted[j].From
	})

	return accepted
}

// calculateWeightedScore computes the weighted sum of confidences from multiple matchers.
// Uses Recipe.Weights to weight each matcher's contribution.
//
// Formula: Σ(weight[matcher] * confidence) for each edge from different matchers.
func (e Ensemble) calculateWeightedScore(edges []LinkEdge) float64 {
	// Group by recipe (matcher output) to avoid double-counting
	scoreByRecipe := make(map[string]float64)

	for _, edge := range edges {
		// Extract matcher name from recipe (format: "matcher_name/v1")
		matcherName := edge.Recipe
		if idx := strings.Index(edge.Recipe, "/"); idx > 0 {
			matcherName = edge.Recipe[:idx]
		}

		// Get weight for this matcher (default to 0 if not in recipe)
		weight, hasWeight := e.Recipe.Weights[matcherName]
		if !hasWeight {
			weight = 0.0
		}

		// Keep the highest confidence from this matcher
		weightedScore := weight * edge.Confidence
		if weightedScore > scoreByRecipe[matcherName] {
			scoreByRecipe[matcherName] = weightedScore
		}
	}

	// Sum all weighted scores
	var total float64
	for _, score := range scoreByRecipe {
		total += score
	}

	return total
}

// generateCombinedProof creates a cryptographic proof over all contributing proofs.
// This makes the ensemble decision verifiable and auditable.
//
// Privacy: All inputs are already synthetic proofs (HMAC hashes). No PII.
func (e Ensemble) generateCombinedProof(candidate *candidateEvidence) (string, error) {
	// Sort proofs for deterministic output
	sortedProofs := make([]string, len(candidate.Proofs))
	copy(sortedProofs, candidate.Proofs)
	sort.Strings(sortedProofs)

	// Concatenate all proofs with the candidate key
	input := fmt.Sprintf("%s|%s|%s|ensemble|%s",
		candidate.Key.From,
		candidate.Key.To,
		candidate.Key.Kind,
		strings.Join(sortedProofs, ","),
	)

	proof, err := crypto.HMACSHA256Hex(e.TenantSalt, input)
	if err != nil {
		return "", err
	}

	return proof, nil
}

// selectBestProof returns the proof from the highest-confidence edge.
// Used as a fallback if combined proof generation fails.
func (e Ensemble) selectBestProof(edges []LinkEdge) string {
	if len(edges) == 0 {
		return ""
	}

	bestEdge := edges[0]
	for _, edge := range edges[1:] {
		if edge.Confidence > bestEdge.Confidence {
			bestEdge = edge
		}
	}

	return bestEdge.Proof
}

// consolidateNotes combines notes from all contributing matchers into a short summary.
// Ensures the result is under 200 characters and contains no PII.
func (e Ensemble) consolidateNotes(notes []string, combinedScore float64) string {
	if len(notes) == 0 {
		return fmt.Sprintf("ensemble match (score=%.2f)", combinedScore)
	}

	// Deduplicate notes
	uniqueNotes := make(map[string]bool)
	for _, note := range notes {
		if note != "" {
			uniqueNotes[note] = true
		}
	}

	// Join unique notes
	var parts []string
	for note := range uniqueNotes {
		parts = append(parts, note)
	}
	sort.Strings(parts) // Deterministic order

	joined := strings.Join(parts, "; ")

	// Add combined score
	result := fmt.Sprintf("%s (score=%.2f)", joined, combinedScore)

	// Truncate if too long (max 200 chars)
	if len(result) > 200 {
		result = result[:197] + "..."
	}

	return result
}

// RunEnsemble is a convenience method that runs all matchers and combines their results.
// Returns only the edges that pass the ensemble threshold.
func (e Ensemble) RunEnsemble(usages []UsageEvent, subs []SubscriptionLite, invs []InvoiceLite) ([]LinkEdge, error) {
	// Run all matchers
	allEdges, err := e.Match(usages, subs, invs)
	if err != nil {
		return nil, err
	}

	// Combine and filter
	accepted := e.Combine(allEdges)

	return accepted, nil
}
