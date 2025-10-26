package stripec

import (
	"linker-agent/internal/matchers"
	"linker-agent/internal/models"
)

// ToSubscriptionLite converts a sanitized Subscription model to a matcher-friendly SubscriptionLite.
// All IDs are already synthetic from the sanitization process.
//
// Privacy: All IDs are synthetic. No PII.
func ToSubscriptionLite(sub models.Subscription) matchers.SubscriptionLite {
	// Extract price IDs and product IDs from items
	var priceIDs []string
	var productIDs []string

	for _, item := range sub.Items {
		if item.PriceID != "" {
			priceIDs = append(priceIDs, item.PriceID)
		}
		if item.ProductID != "" {
			productIDs = append(productIDs, item.ProductID)
		}
	}

	return matchers.SubscriptionLite{
		ID:         sub.ID,
		Customer:   string(sub.Customer), // Convert SynStripeID to string
		PriceIDs:   priceIDs,
		ProductIDs: productIDs,
		CreatedAt:  sub.CreatedAt,
		Status:     sub.Status,
	}
}

// ToInvoiceLite converts a sanitized Invoice model to a matcher-friendly InvoiceLite.
// All IDs are already synthetic from the sanitization process.
//
// Privacy: All IDs are synthetic. No PII.
func ToInvoiceLite(inv models.Invoice) matchers.InvoiceLite {
	return matchers.InvoiceLite{
		ID:           inv.ID,
		Customer:     string(inv.Customer), // Convert SynStripeID to string
		Subscription: inv.Subscription,
		Total:        inv.Total,
		Currency:     inv.Currency,
		Status:       inv.Status,
		CreatedAt:    inv.CreatedAt,
		Paid:         inv.Paid,
	}
}

// ToSubscriptionLites converts a slice of sanitized Subscriptions to SubscriptionLites.
func ToSubscriptionLites(subs []models.Subscription) []matchers.SubscriptionLite {
	lites := make([]matchers.SubscriptionLite, len(subs))
	for i, sub := range subs {
		lites[i] = ToSubscriptionLite(sub)
	}
	return lites
}

// ToInvoiceLites converts a slice of sanitized Invoices to InvoiceLites.
func ToInvoiceLites(invs []models.Invoice) []matchers.InvoiceLite {
	lites := make([]matchers.InvoiceLite, len(invs))
	for i, inv := range invs {
		lites[i] = ToInvoiceLite(inv)
	}
	return lites
}
