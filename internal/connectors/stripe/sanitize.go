package stripec

import (
	"github.com/stripe/stripe-go/v83"
	"linker-agent/internal/models"
)

// SanitizeSubscription maps a Stripe subscription to a PII-free models.Subscription.
// This function operates on a whitelist-only basis: if Stripe adds new fields,
// they are ignored by default, ensuring no accidental PII leakage.
//
// Allowed fields:
//   - ID (opaque identifier)
//   - Customer ID (opaque identifier, to be HMACed by caller)
//   - Status (system enum)
//   - Items (price ID, product ID, quantity)
//   - Created (Unix timestamp)
//
// Explicitly omitted (PII or unnecessary):
//   - Customer object (email, name, address, phone, metadata)
//   - Description, metadata, notes
//   - Billing details, shipping, tax info
//   - Discount, coupon details
//   - Payment method information
//
// Security: The customer ID returned is the raw Stripe customer ID.
// The caller MUST apply HMAC before storing or transmitting this value.
func SanitizeSubscription(s *stripe.Subscription) models.Subscription {
	var items []models.SubItem
	for _, item := range s.Items.Data {
		// Extract only the IDs and quantity, no pricing or description
		items = append(items, models.SubItem{
			PriceID:   item.Price.ID,
			ProductID: item.Price.Product.ID,
			Quantity:  item.Quantity,
		})
	}

	return models.Subscription{
		ID:        s.ID,
		Customer:  models.SynStripeID(s.Customer.ID),
		Status:    string(s.Status),
		Items:     items,
		CreatedAt: s.Created,
	}
}

// SanitizeInvoice maps a Stripe invoice to a PII-free models.Invoice.
// This function operates on a whitelist-only basis: if Stripe adds new fields,
// they are ignored by default, ensuring no accidental PII leakage.
//
// Allowed fields:
//   - ID (opaque identifier)
//   - Customer ID (opaque identifier, to be HMACed by caller)
//   - Subscription ID (opaque identifier, may be empty)
//   - Total (amount in minor units)
//   - Currency (ISO code)
//   - Status (system enum)
//   - Created (Unix timestamp)
//   - Paid (boolean derived from status)
//
// Explicitly omitted (PII or unnecessary):
//   - Customer object (email, name, address, phone, metadata)
//   - Billing details, shipping, tax details
//   - Description, footer, statement descriptor
//   - Line items with descriptions
//   - Payment intent details
//   - Discount, coupon, promotion codes
//
// Security: The customer ID returned is the raw Stripe customer ID.
// The caller MUST apply HMAC before storing or transmitting this value.
//
// Note: The subscription ID is extracted from invoice lines if present.
// Not all invoices are associated with subscriptions.
func SanitizeInvoice(i *stripe.Invoice) models.Invoice {
	// Derive paid status from invoice status
	paid := i.Status == stripe.InvoiceStatusPaid

	// Extract subscription ID from lines if present
	// Invoices may or may not be associated with a subscription
	subscriptionID := ""
	if i.Lines != nil && len(i.Lines.Data) > 0 {
		for _, line := range i.Lines.Data {
			if line.Subscription != nil {
				subscriptionID = line.Subscription.ID
				break
			}
		}
	}

	return models.Invoice{
		ID:           i.ID,
		Customer:     models.SynStripeID(i.Customer.ID),
		Subscription: subscriptionID,
		Total:        i.Total,
		Currency:     string(i.Currency),
		Status:       string(i.Status),
		CreatedAt:    i.Created,
		Paid:         paid,
	}
}
