package matchers

import (
	"fmt"
	"linker-agent/internal/crypto"
)

// DeterministicIDMatcher performs exact joins on synthetic IDs to create
// high-confidence (1.0) links between users and their subscriptions/invoices.
//
// Matching logic:
// - If UsageEvent.User == SubscriptionLite.Customer → user->subscription edge
// - If UsageEvent.User == InvoiceLite.Customer → user->invoice edge
// - If UsageEvent.SKU matches subscription PriceIDs/ProductIDs → additional confidence
//
// Privacy: All IDs are synthetic. Proofs are HMAC hashes over synthetic inputs.
type DeterministicIDMatcher struct {
	TenantSalt []byte // Used for generating cryptographic proofs
}

// Name returns the unique identifier for this matcher.
func (m DeterministicIDMatcher) Name() string {
	return "deterministic_id"
}

// Match performs deterministic ID-based matching between usage events and billing data.
// Returns link edges with confidence 1.0 for exact ID matches.
func (m DeterministicIDMatcher) Match(usages []UsageEvent, subs []SubscriptionLite, invs []InvoiceLite) ([]LinkEdge, error) {
	var edges []LinkEdge

	// Build indexes for fast lookups
	subsByCustomer := make(map[string][]SubscriptionLite)
	for _, sub := range subs {
		subsByCustomer[sub.Customer] = append(subsByCustomer[sub.Customer], sub)
	}

	invsByCustomer := make(map[string][]InvoiceLite)
	for _, inv := range invs {
		invsByCustomer[inv.Customer] = append(invsByCustomer[inv.Customer], inv)
	}

	// Process each usage event
	for _, usage := range usages {
		// Match user -> subscriptions (exact customer ID match)
		if userSubs, found := subsByCustomer[usage.User]; found {
			for _, sub := range userSubs {
				proof, err := m.generateProof(usage.User, sub.ID, "user->subscription", usage.SKU)
				if err != nil {
					return nil, fmt.Errorf("failed to generate proof: %w", err)
				}

				edges = append(edges, LinkEdge{
					From:       usage.User,
					To:         sub.ID,
					Kind:       "user->subscription",
					Confidence: 1.0,
					Proof:      proof,
					Recipe:     "deterministic_id/v1",
					At:         usage.At,
					Notes:      "exact customer ID match",
				})

				// Check SKU overlap for additional edges to prices/products
				for _, priceID := range sub.PriceIDs {
					if usage.SKU == priceID {
						priceProof, err := m.generateProof(usage.User, priceID, "user->price", usage.SKU)
						if err != nil {
							return nil, fmt.Errorf("failed to generate price proof: %w", err)
						}

						edges = append(edges, LinkEdge{
							From:       usage.User,
							To:         priceID,
							Kind:       "user->price",
							Confidence: 1.0,
							Proof:      priceProof,
							Recipe:     "deterministic_id/v1",
							At:         usage.At,
							Notes:      "exact SKU-price match",
						})
					}
				}

				for _, prodID := range sub.ProductIDs {
					if usage.SKU == prodID {
						prodProof, err := m.generateProof(usage.User, prodID, "user->product", usage.SKU)
						if err != nil {
							return nil, fmt.Errorf("failed to generate product proof: %w", err)
						}

						edges = append(edges, LinkEdge{
							From:       usage.User,
							To:         prodID,
							Kind:       "user->product",
							Confidence: 1.0,
							Proof:      prodProof,
							Recipe:     "deterministic_id/v1",
							At:         usage.At,
							Notes:      "exact SKU-product match",
						})
					}
				}
			}
		}

		// Match user -> invoices (exact customer ID match)
		if userInvs, found := invsByCustomer[usage.User]; found {
			for _, inv := range userInvs {
				proof, err := m.generateProof(usage.User, inv.ID, "user->invoice", usage.SKU)
				if err != nil {
					return nil, fmt.Errorf("failed to generate invoice proof: %w", err)
				}

				edges = append(edges, LinkEdge{
					From:       usage.User,
					To:         inv.ID,
					Kind:       "user->invoice",
					Confidence: 1.0,
					Proof:      proof,
					Recipe:     "deterministic_id/v1",
					At:         usage.At,
					Notes:      "exact customer ID match",
				})
			}
		}
	}

	return edges, nil
}

// generateProof creates a cryptographic proof (HMAC-SHA256) for a link edge.
// The proof is deterministic and verifiable but reveals no sensitive information.
//
// Privacy: All inputs are already synthetic IDs. The proof is a one-way hash.
func (m DeterministicIDMatcher) generateProof(from, to, kind, sku string) (string, error) {
	// Concatenate inputs for proof generation
	input := fmt.Sprintf("%s|%s|%s|%s|%s", from, to, kind, m.Name(), sku)
	proof, err := crypto.HMACSHA256Hex(m.TenantSalt, input)
	if err != nil {
		return "", err
	}
	return proof, nil
}
