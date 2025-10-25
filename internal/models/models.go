// Package models defines PII-free data types used throughout the Connektn Linker Agent.
// These types are safe to serialize, log (at info level), and export to Connektn Cloud
// because they contain ONLY:
//   - Opaque synthetic identifiers (HMAC-derived, never raw IDs)
//   - Numeric amounts, timestamps, and quantities
//   - System-controlled enums (status, currency codes)
//
// Security: NO fields for names, emails, addresses, phone numbers, IP addresses,
// or free-form user-provided text (metadata, descriptions, notes) are permitted.
//
// Threat model: These models are the boundary between untrusted external data
// (Stripe API, analytics platforms) and the privacy-safe internal representation.
// All sanitization happens at the connector layer before data enters these types.
package models

// SynUserID is an opaque synthetic identifier for a user, derived from raw IDs
// using HMAC with a tenant-specific salt. Never parse or derive meaning from this value.
//
// Example: "a3f8c9d2e1b4f7a6c8d5e9f2a1b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1"
type SynUserID string

// SynStripeID is an opaque synthetic identifier for a Stripe resource (customer,
// subscription, invoice, etc.), derived using HMAC with a tenant-specific salt.
// Never parse or derive meaning from this value.
//
// Example: "b2e9d7f1c4a8b6e3d9f5a2c7b1e4d8f3a9c6b2e5d1f7a3c8b4e9d6f2a1c5b8"
type SynStripeID string

// SubItem represents a single line item within a subscription, containing
// only product/price identifiers and quantity. No pricing information is included.
type SubItem struct {
	// PriceID is the Stripe price ID (e.g., "price_1A2B3C4D5E6F7G8H").
	// This is an opaque Stripe identifier, not customer data.
	PriceID string `json:"price_id"`

	// ProductID is the Stripe product ID (e.g., "prod_1A2B3C4D5E6F7G8H").
	// This is an opaque Stripe identifier, not customer data.
	ProductID string `json:"product_id"`

	// Quantity is the number of units of this item in the subscription.
	Quantity int64 `json:"quantity"`
}

// Subscription represents a sanitized Stripe subscription with no PII.
// This is the privacy-safe internal representation used for reconciliation.
type Subscription struct {
	// ID is the Stripe subscription ID (e.g., "sub_1A2B3C4D5E6F7G8H").
	// This is an opaque Stripe identifier, not customer data.
	ID string `json:"id"`

	// Customer is the synthetic Stripe customer ID (HMAC-derived).
	// This is a privacy-preserving identifier that cannot be reversed to the original.
	Customer SynStripeID `json:"customer"`

	// Status is the subscription status enum from Stripe.
	// Possible values: "active", "canceled", "incomplete", "incomplete_expired",
	// "past_due", "trialing", "unpaid", "paused".
	Status string `json:"status"`

	// Items is the list of subscription line items (prices, products, quantities).
	// This contains only IDs and quantities, no customer data.
	Items []SubItem `json:"items"`

	// CreatedAt is the subscription creation timestamp in Unix seconds (UTC).
	CreatedAt int64 `json:"created_at"`
}

// Invoice represents a sanitized Stripe invoice with no PII.
// This is the privacy-safe internal representation used for reconciliation.
type Invoice struct {
	// ID is the Stripe invoice ID (e.g., "in_1A2B3C4D5E6F7G8H").
	// This is an opaque Stripe identifier, not customer data.
	ID string `json:"id"`

	// Customer is the synthetic Stripe customer ID (HMAC-derived).
	// This is a privacy-preserving identifier that cannot be reversed to the original.
	Customer SynStripeID `json:"customer"`

	// Subscription is the Stripe subscription ID associated with this invoice.
	// Empty string if the invoice is not associated with a subscription.
	// This is an opaque Stripe identifier, not customer data.
	Subscription string `json:"subscription"`

	// Total is the invoice total amount in currency minor units.
	// For USD, this is cents (e.g., 1000 = $10.00).
	// For zero-decimal currencies (JPY, KRW), this is the actual amount.
	Total int64 `json:"total"`

	// Currency is the three-letter ISO currency code (lowercase).
	// Examples: "usd", "eur", "gbp", "jpy"
	Currency string `json:"currency"`

	// Status is the invoice status enum from Stripe.
	// Possible values: "draft", "open", "paid", "void", "uncollectible"
	Status string `json:"status"`

	// CreatedAt is the invoice creation timestamp in Unix seconds (UTC).
	CreatedAt int64 `json:"created_at"`

	// Paid indicates whether the invoice has been paid.
	// This is derived from Status == "paid" for convenience.
	Paid bool `json:"paid"`
}
