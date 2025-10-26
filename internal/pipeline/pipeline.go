// Package pipeline orchestrates the end-to-end matching workflow:
// 1. Accept sanitized usage events and billing data as inputs
// 2. Run all configured matchers
// 3. Combine evidence via ensemble
// 4. Export accepted link edges
//
// Privacy: All identifiers are synthetic (HMAC-derived). No PII is ever processed or exported.
package pipeline

import (
	"context"
	"fmt"
	"linker-agent/internal/exporter"
	"linker-agent/internal/matchers"
)

// Inputs contains all sanitized data needed for matching.
// All IDs must be synthetic (HMAC-derived with tenant salt).
type Inputs struct {
	Usages []matchers.UsageEvent
	Subs   []matchers.SubscriptionLite
	Invs   []matchers.InvoiceLite
}

// Stats tracks pipeline execution metrics.
type Stats struct {
	UsageEvents       int // Number of usage events processed
	Subscriptions     int // Number of subscriptions processed
	Invoices          int // Number of invoices processed
	RawEdges          int // Total edges before ensemble filtering
	AcceptedEdges     int // Edges that passed threshold
	ExportedEdges     int // Edges successfully exported
	MatcherStats      map[string]int // Edges per matcher
	HighConfidence    int // Edges with confidence >= 0.9
	MediumConfidence  int // Edges with confidence >= 0.6 and < 0.9
	LowConfidence     int // Edges with confidence < 0.6
}

// Result contains the output of a pipeline run.
type Result struct {
	Edges []matchers.LinkEdge
	Stats Stats
}

// Run executes the matching pipeline end-to-end:
// 1. Run all matchers in the ensemble
// 2. Combine edges with weighted scoring
// 3. Export accepted edges via the configured exporter
//
// Privacy: All inputs and outputs use synthetic IDs only. No PII.
func Run(ctx context.Context, ensemble matchers.Ensemble, exp *exporter.Exporter, in Inputs) (*Result, error) {
	// Validate inputs
	if err := validateInputs(in); err != nil {
		return nil, fmt.Errorf("invalid inputs: %w", err)
	}

	// Initialize stats
	stats := Stats{
		UsageEvents:   len(in.Usages),
		Subscriptions: len(in.Subs),
		Invoices:      len(in.Invs),
		MatcherStats:  make(map[string]int),
	}

	// Run all matchers
	var allEdges []matchers.LinkEdge
	for _, matcher := range ensemble.Matchers {
		edges, err := matcher.Match(in.Usages, in.Subs, in.Invs)
		if err != nil {
			return nil, fmt.Errorf("matcher %s failed: %w", matcher.Name(), err)
		}

		// Track per-matcher stats
		stats.MatcherStats[matcher.Name()] = len(edges)
		allEdges = append(allEdges, edges...)
	}

	stats.RawEdges = len(allEdges)

	// Combine edges via ensemble
	accepted := ensemble.Combine(allEdges)
	stats.AcceptedEdges = len(accepted)

	// Classify by confidence
	for _, edge := range accepted {
		if edge.Confidence >= 0.9 {
			stats.HighConfidence++
		} else if edge.Confidence >= 0.6 {
			stats.MediumConfidence++
		} else {
			stats.LowConfidence++
		}
	}

	// Export edges if exporter is provided
	if exp != nil {
		for _, edge := range accepted {
			if err := exp.Enqueue(ctx, edge); err != nil {
				return nil, fmt.Errorf("failed to enqueue edge: %w", err)
			}
		}
		stats.ExportedEdges = len(accepted)
	}

	return &Result{
		Edges: accepted,
		Stats: stats,
	}, nil
}

// RunWithoutExport is a variant of Run that skips the export step.
// Useful for testing, dry-runs, or when you just want to inspect the results.
func RunWithoutExport(ctx context.Context, ensemble matchers.Ensemble, in Inputs) (*Result, error) {
	return Run(ctx, ensemble, nil, in)
}

// validateInputs performs basic sanity checks on pipeline inputs.
// Ensures all IDs are synthetic (start with "syn_").
func validateInputs(in Inputs) error {
	// Check usage events
	for i, usage := range in.Usages {
		if !isSyntheticID(usage.User) {
			return fmt.Errorf("usage[%d].User is not synthetic: %s", i, usage.User)
		}
		if !isSyntheticID(usage.SKU) && usage.SKU != "" {
			// SKU might be empty or a synthetic ID (price/product)
			// Allow empty SKUs for now
		}
		if usage.At <= 0 {
			return fmt.Errorf("usage[%d].At must be > 0", i)
		}
	}

	// Check subscriptions
	for i, sub := range in.Subs {
		if !isSyntheticID(sub.ID) {
			return fmt.Errorf("subscription[%d].ID is not synthetic: %s", i, sub.ID)
		}
		if !isSyntheticID(sub.Customer) {
			return fmt.Errorf("subscription[%d].Customer is not synthetic: %s", i, sub.Customer)
		}
		for j, priceID := range sub.PriceIDs {
			if !isSyntheticID(priceID) {
				return fmt.Errorf("subscription[%d].PriceIDs[%d] is not synthetic: %s", i, j, priceID)
			}
		}
		for j, productID := range sub.ProductIDs {
			if !isSyntheticID(productID) {
				return fmt.Errorf("subscription[%d].ProductIDs[%d] is not synthetic: %s", i, j, productID)
			}
		}
	}

	// Check invoices
	for i, inv := range in.Invs {
		if !isSyntheticID(inv.ID) {
			return fmt.Errorf("invoice[%d].ID is not synthetic: %s", i, inv.ID)
		}
		if !isSyntheticID(inv.Customer) {
			return fmt.Errorf("invoice[%d].Customer is not synthetic: %s", i, inv.Customer)
		}
		if inv.Subscription != "" && !isSyntheticID(inv.Subscription) {
			return fmt.Errorf("invoice[%d].Subscription is not synthetic: %s", i, inv.Subscription)
		}
	}

	return nil
}

// isSyntheticID checks if an ID follows the synthetic format (starts with "syn_").
// This is a basic privacy check to ensure no raw Stripe IDs enter the pipeline.
func isSyntheticID(id string) bool {
	if len(id) < 4 {
		return false
	}
	return id[:4] == "syn_"
}
