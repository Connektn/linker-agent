package stripec

import (
	"github.com/stripe/stripe-go/v83"
	"linker-agent/internal/crypto"
	"linker-agent/internal/models"
)

// SanitizeSubscription maps a Stripe subscription to a PII-free models.Subscription.
// This function operates on a whitelist-only basis: if Stripe adds new fields,
// they are ignored by default, ensuring no accidental PII leakage.
//
// All identifiers (subscription ID, customer ID, price IDs, product IDs) are
// converted to synthetic IDs using HMAC-SHA256 with tenant-specific salt.
//
// Allowed fields:
//   - ID (converted to synthetic ID with prefix syn_sub)
//   - Customer ID (converted to synthetic ID with prefix syn_cust)
//   - Status (system enum)
//   - Items (price ID, product ID, quantity - IDs converted to synthetic)
//   - Created (Unix timestamp)
//
// Explicitly omitted (PII or unnecessary):
//   - Customer object (email, name, address, phone, metadata)
//   - Description, metadata, notes
//   - Billing details, shipping, tax info
//   - Discount, coupon details
//   - Payment method information
//
// Security: All IDs are converted to synthetic IDs before export.
// No raw Stripe IDs are returned.
//
// Threat model: Prevents customer re-identification via subscription, price,
// or product IDs by ensuring all identifiers are synthetic and scoped to tenant.
func SanitizeSubscription(salt []byte, s *stripe.Subscription) (models.Subscription, error) {
	// Generate synthetic subscription ID
	id, err := crypto.SyntheticID(salt, s.ID, "syn_sub")
	if err != nil {
		return models.Subscription{}, err
	}

	// Generate synthetic customer ID
	cust, err := crypto.SyntheticID(salt, s.Customer.ID, "syn_cust")
	if err != nil {
		return models.Subscription{}, err
	}

	// Process subscription items and synthesize price/product IDs
	items := make([]models.SubItem, 0, len(s.Items.Data))
	for _, item := range s.Items.Data {
		var priceID, prodID string
		if item.Price != nil {
			if item.Price.ID != "" {
				priceID, _ = crypto.SyntheticID(salt, item.Price.ID, "syn_price")
			}
			if item.Price.Product != nil && item.Price.Product.ID != "" {
				prodID, _ = crypto.SyntheticID(salt, item.Price.Product.ID, "syn_prod")
			}
		}
		items = append(items, models.SubItem{
			PriceID:   priceID,
			ProductID: prodID,
			Quantity:  item.Quantity,
		})
	}

	return models.Subscription{
		ID:        id,
		Customer:  models.SynStripeID(cust),
		Status:    string(s.Status),
		Items:     items,
		CreatedAt: s.Created,
	}, nil
}

// SanitizeInvoice maps a Stripe invoice to a PII-free models.Invoice.
// This function operates on a whitelist-only basis: if Stripe adds new fields,
// they are ignored by default, ensuring no accidental PII leakage.
//
// All identifiers (invoice ID, customer ID, subscription ID) are
// converted to synthetic IDs using HMAC-SHA256 with tenant-specific salt.
//
// Allowed fields:
//   - ID (converted to synthetic ID with prefix syn_inv)
//   - Customer ID (converted to synthetic ID with prefix syn_cust)
//   - Subscription ID (converted to synthetic ID with prefix syn_sub, may be empty)
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
// Security: All IDs are converted to synthetic IDs before export.
// No raw Stripe IDs are returned.
//
// Threat model: Prevents customer re-identification via invoice or subscription
// IDs by ensuring all identifiers are synthetic and scoped to tenant.
//
// Note: The subscription ID is extracted from invoice lines if present.
// Not all invoices are associated with subscriptions.
func SanitizeInvoice(salt []byte, i *stripe.Invoice) (models.Invoice, error) {
	// Generate synthetic invoice ID
	id, err := crypto.SyntheticID(salt, i.ID, "syn_inv")
	if err != nil {
		return models.Invoice{}, err
	}

	// Generate synthetic customer ID
	cust, err := crypto.SyntheticID(salt, i.Customer.ID, "syn_cust")
	if err != nil {
		return models.Invoice{}, err
	}

	// Derive paid status from invoice status
	paid := i.Status == stripe.InvoiceStatusPaid

	// Extract and synthesize subscription ID from lines if present
	// Invoices may or may not be associated with a subscription
	subscriptionID := ""
	if i.Lines != nil && len(i.Lines.Data) > 0 {
		for _, line := range i.Lines.Data {
			if line.Subscription != nil && line.Subscription.ID != "" {
				subscriptionID, err = crypto.SyntheticID(salt, line.Subscription.ID, "syn_sub")
				if err != nil {
					return models.Invoice{}, err
				}
				break
			}
		}
	}

	return models.Invoice{
		ID:           id,
		Customer:     models.SynStripeID(cust),
		Subscription: subscriptionID,
		Total:        i.Total,
		Currency:     string(i.Currency),
		Status:       string(i.Status),
		CreatedAt:    i.Created,
		Paid:         paid,
	}, nil
}
