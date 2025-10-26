package matchers

import (
	"fmt"
	"linker-agent/internal/crypto"
)

// SKUOverlapMatcher correlates usage events with subscriptions based on SKU/product overlap.
// If a usage event's SKU matches a subscription's price or product IDs, it suggests the
// user is using features they've purchased.
//
// Matching logic:
// - If UsageEvent.SKU matches any subscription PriceID → user->subscription edge (high confidence)
// - If UsageEvent.SKU matches any subscription ProductID → user->subscription edge (high confidence)
// - Confidence is 0.8 for exact SKU matches
//
// Privacy: All IDs are synthetic. Proofs include SKU (which is a synthetic ID).
type SKUOverlapMatcher struct {
	TenantSalt []byte // Used for generating cryptographic proofs
}

// Name returns the unique identifier for this matcher.
func (m SKUOverlapMatcher) Name() string {
	return "sku_overlap"
}

// Match performs SKU overlap matching between usage events and subscriptions.
// Returns link edges with high confidence (0.8) for SKU matches.
func (m SKUOverlapMatcher) Match(usages []UsageEvent, subs []SubscriptionLite, invs []InvoiceLite) ([]LinkEdge, error) {
	var edges []LinkEdge

	// Build index: SKU -> subscriptions that include this price/product
	skuToSubs := make(map[string][]SubscriptionLite)
	for _, sub := range subs {
		// Index by price IDs
		for _, priceID := range sub.PriceIDs {
			skuToSubs[priceID] = append(skuToSubs[priceID], sub)
		}
		// Index by product IDs
		for _, prodID := range sub.ProductIDs {
			skuToSubs[prodID] = append(skuToSubs[prodID], sub)
		}
	}

	// Process each usage event
	for _, usage := range usages {
		// Check if usage SKU matches any subscription's prices/products
		if matchingSubs, found := skuToSubs[usage.SKU]; found {
			for _, sub := range matchingSubs {
				// Determine which type of match (price or product)
				matchType := ""
				for _, priceID := range sub.PriceIDs {
					if usage.SKU == priceID {
						matchType = "price"
						break
					}
				}
				if matchType == "" {
					for _, prodID := range sub.ProductIDs {
						if usage.SKU == prodID {
							matchType = "product"
							break
						}
					}
				}

				proof, err := m.generateProof(usage.User, sub.ID, "user->subscription", usage.SKU)
				if err != nil {
					return nil, fmt.Errorf("failed to generate proof: %w", err)
				}

				notes := fmt.Sprintf("SKU matched subscription %s", matchType)
				edges = append(edges, LinkEdge{
					From:       usage.User,
					To:         sub.ID,
					Kind:       "user->subscription",
					Confidence: 0.8, // High confidence for exact SKU match
					Proof:      proof,
					Recipe:     "sku_overlap/v1",
					At:         usage.At,
					Notes:      notes,
				})

				// Also create direct edges to the matched price/product
				if matchType == "price" {
					priceProof, err := m.generateProof(usage.User, usage.SKU, "user->price", usage.SKU)
					if err != nil {
						return nil, fmt.Errorf("failed to generate price proof: %w", err)
					}

					edges = append(edges, LinkEdge{
						From:       usage.User,
						To:         usage.SKU,
						Kind:       "user->price",
						Confidence: 0.9, // Very high confidence for exact price match
						Proof:      priceProof,
						Recipe:     "sku_overlap/v1",
						At:         usage.At,
						Notes:      "exact SKU-price match",
					})
				} else if matchType == "product" {
					prodProof, err := m.generateProof(usage.User, usage.SKU, "user->product", usage.SKU)
					if err != nil {
						return nil, fmt.Errorf("failed to generate product proof: %w", err)
					}

					edges = append(edges, LinkEdge{
						From:       usage.User,
						To:         usage.SKU,
						Kind:       "user->product",
						Confidence: 0.9, // Very high confidence for exact product match
						Proof:      prodProof,
						Recipe:     "sku_overlap/v1",
						At:         usage.At,
						Notes:      "exact SKU-product match",
					})
				}
			}
		}
	}

	return edges, nil
}

// generateProof creates a cryptographic proof (HMAC-SHA256) for an SKU overlap edge.
// The proof includes the SKU to make the match verifiable.
//
// Privacy: All inputs are synthetic IDs. The SKU is also a synthetic identifier.
func (m SKUOverlapMatcher) generateProof(from, to, kind, sku string) (string, error) {
	// Concatenate inputs for proof generation
	input := fmt.Sprintf("%s|%s|%s|%s|%s", from, to, kind, m.Name(), sku)
	proof, err := crypto.HMACSHA256Hex(m.TenantSalt, input)
	if err != nil {
		return "", err
	}
	return proof, nil
}
