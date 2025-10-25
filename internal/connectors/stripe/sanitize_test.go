package stripec

import (
	"testing"

	"github.com/stripe/stripe-go/v83"
	"linker-agent/internal/models"
)

// TestSanitizeSubscription_WhitelistedFields verifies that only approved fields are mapped.
func TestSanitizeSubscription_WhitelistedFields(t *testing.T) {
	// Build a minimal fake Stripe subscription with only required fields
	stripeSubscription := &stripe.Subscription{
		ID: "sub_test123",
		Customer: &stripe.Customer{
			ID: "cus_test456",
		},
		Status:  stripe.SubscriptionStatusActive,
		Created: 1609459200, // 2021-01-01 00:00:00 UTC
		Items: &stripe.SubscriptionItemList{
			Data: []*stripe.SubscriptionItem{
				{
					Price: &stripe.Price{
						ID: "price_test789",
						Product: &stripe.Product{
							ID: "prod_testABC",
						},
					},
					Quantity: 2,
				},
			},
		},
	}

	result := SanitizeSubscription(stripeSubscription)

	// Verify all whitelisted fields are present and correct
	if result.ID != "sub_test123" {
		t.Errorf("Expected ID 'sub_test123', got '%s'", result.ID)
	}

	if result.Customer != models.SynStripeID("cus_test456") {
		t.Errorf("Expected Customer 'cus_test456', got '%s'", result.Customer)
	}

	if result.Status != "active" {
		t.Errorf("Expected Status 'active', got '%s'", result.Status)
	}

	if result.CreatedAt != 1609459200 {
		t.Errorf("Expected CreatedAt 1609459200, got %d", result.CreatedAt)
	}

	if len(result.Items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(result.Items))
	}

	item := result.Items[0]
	if item.PriceID != "price_test789" {
		t.Errorf("Expected PriceID 'price_test789', got '%s'", item.PriceID)
	}

	if item.ProductID != "prod_testABC" {
		t.Errorf("Expected ProductID 'prod_testABC', got '%s'", item.ProductID)
	}

	if item.Quantity != 2 {
		t.Errorf("Expected Quantity 2, got %d", item.Quantity)
	}
}

// TestSanitizeSubscription_MultipleItems verifies that multiple subscription items are mapped correctly.
func TestSanitizeSubscription_MultipleItems(t *testing.T) {
	stripeSubscription := &stripe.Subscription{
		ID: "sub_multi",
		Customer: &stripe.Customer{
			ID: "cus_multi",
		},
		Status:  stripe.SubscriptionStatusActive,
		Created: 1609459200,
		Items: &stripe.SubscriptionItemList{
			Data: []*stripe.SubscriptionItem{
				{
					Price: &stripe.Price{
						ID: "price_1",
						Product: &stripe.Product{
							ID: "prod_1",
						},
					},
					Quantity: 1,
				},
				{
					Price: &stripe.Price{
						ID: "price_2",
						Product: &stripe.Product{
							ID: "prod_2",
						},
					},
					Quantity: 5,
				},
			},
		},
	}

	result := SanitizeSubscription(stripeSubscription)

	if len(result.Items) != 2 {
		t.Fatalf("Expected 2 items, got %d", len(result.Items))
	}

	// Verify first item
	if result.Items[0].PriceID != "price_1" || result.Items[0].Quantity != 1 {
		t.Errorf("First item mismatch: got %+v", result.Items[0])
	}

	// Verify second item
	if result.Items[1].PriceID != "price_2" || result.Items[1].Quantity != 5 {
		t.Errorf("Second item mismatch: got %+v", result.Items[1])
	}
}

// TestSanitizeSubscription_NoPIILeakage verifies that PII fields are not copied.
// This test constructs a Stripe subscription with PII fields and ensures they don't appear in the result.
func TestSanitizeSubscription_NoPIILeakage(t *testing.T) {
	// Build a subscription with PII fields populated
	stripeSubscription := &stripe.Subscription{
		ID: "sub_nopii",
		Customer: &stripe.Customer{
			ID:    "cus_nopii",
			Email: "customer@example.com", // PII
			Name:  "John Doe",             // PII
		},
		Status:  stripe.SubscriptionStatusActive,
		Created: 1609459200,
		Items: &stripe.SubscriptionItemList{
			Data: []*stripe.SubscriptionItem{
				{
					Price: &stripe.Price{
						ID: "price_test",
						Product: &stripe.Product{
							ID: "prod_test",
						},
					},
					Quantity: 1,
				},
			},
		},
		// Note: We're not populating metadata, description, etc. because
		// the Stripe SDK types don't expose them on the Subscription struct
		// in a way that would leak. But the key point is that our sanitizer
		// uses a whitelist approach, so even if they were present, they wouldn't be copied.
	}

	result := SanitizeSubscription(stripeSubscription)

	// The result should only have the customer ID, not the full customer object
	// We verify this by checking that only the ID is present (as a SynStripeID)
	if result.Customer != models.SynStripeID("cus_nopii") {
		t.Errorf("Expected only customer ID, got %+v", result.Customer)
	}

	// The models.Subscription struct has no fields for email, name, etc.
	// If we were to add them, this test would fail at compile time.
}

// TestSanitizeInvoice_WhitelistedFields verifies that only approved fields are mapped.
func TestSanitizeInvoice_WhitelistedFields(t *testing.T) {
	// Build a minimal fake Stripe invoice
	stripeInvoice := &stripe.Invoice{
		ID: "in_test123",
		Customer: &stripe.Customer{
			ID: "cus_test456",
		},
		Total:    10000, // $100.00 in cents
		Currency: stripe.CurrencyUSD,
		Status:   stripe.InvoiceStatusPaid,
		Created:  1609459200,
		Lines: &stripe.InvoiceLineItemList{
			Data: []*stripe.InvoiceLineItem{
				{
					Subscription: &stripe.Subscription{
						ID: "sub_test789",
					},
				},
			},
		},
	}

	result := SanitizeInvoice(stripeInvoice)

	// Verify all whitelisted fields are present and correct
	if result.ID != "in_test123" {
		t.Errorf("Expected ID 'in_test123', got '%s'", result.ID)
	}

	if result.Customer != models.SynStripeID("cus_test456") {
		t.Errorf("Expected Customer 'cus_test456', got '%s'", result.Customer)
	}

	if result.Subscription != "sub_test789" {
		t.Errorf("Expected Subscription 'sub_test789', got '%s'", result.Subscription)
	}

	if result.Total != 10000 {
		t.Errorf("Expected Total 10000, got %d", result.Total)
	}

	if result.Currency != "usd" {
		t.Errorf("Expected Currency 'usd', got '%s'", result.Currency)
	}

	if result.Status != "paid" {
		t.Errorf("Expected Status 'paid', got '%s'", result.Status)
	}

	if result.CreatedAt != 1609459200 {
		t.Errorf("Expected CreatedAt 1609459200, got %d", result.CreatedAt)
	}

	if !result.Paid {
		t.Errorf("Expected Paid true, got false")
	}
}

// TestSanitizeInvoice_PaidStatus verifies that the Paid field is correctly derived from Status.
func TestSanitizeInvoice_PaidStatus(t *testing.T) {
	tests := []struct {
		name   string
		status stripe.InvoiceStatus
		paid   bool
	}{
		{"paid invoice", stripe.InvoiceStatusPaid, true},
		{"open invoice", stripe.InvoiceStatusOpen, false},
		{"draft invoice", stripe.InvoiceStatusDraft, false},
		{"void invoice", stripe.InvoiceStatusVoid, false},
		{"uncollectible invoice", stripe.InvoiceStatusUncollectible, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stripeInvoice := &stripe.Invoice{
				ID: "in_test",
				Customer: &stripe.Customer{
					ID: "cus_test",
				},
				Total:    1000,
				Currency: stripe.CurrencyUSD,
				Status:   tt.status,
				Created:  1609459200,
			}

			result := SanitizeInvoice(stripeInvoice)

			if result.Paid != tt.paid {
				t.Errorf("Expected Paid %v for status %s, got %v", tt.paid, tt.status, result.Paid)
			}
		})
	}
}

// TestSanitizeInvoice_NoSubscription verifies handling of invoices without a subscription.
func TestSanitizeInvoice_NoSubscription(t *testing.T) {
	stripeInvoice := &stripe.Invoice{
		ID: "in_no_sub",
		Customer: &stripe.Customer{
			ID: "cus_test",
		},
		Total:    5000,
		Currency: stripe.CurrencyUSD,
		Status:   stripe.InvoiceStatusOpen,
		Created:  1609459200,
		// No Lines or empty Lines
	}

	result := SanitizeInvoice(stripeInvoice)

	if result.Subscription != "" {
		t.Errorf("Expected empty Subscription, got '%s'", result.Subscription)
	}
}

// TestSanitizeInvoice_MinorUnits verifies that amounts are preserved in minor units.
func TestSanitizeInvoice_MinorUnits(t *testing.T) {
	tests := []struct {
		name     string
		total    int64
		currency stripe.Currency
		expected int64
	}{
		{"USD cents", 12345, stripe.CurrencyUSD, 12345},       // $123.45
		{"EUR cents", 50000, stripe.CurrencyEUR, 50000},       // €500.00
		{"JPY no decimals", 1000, stripe.CurrencyJPY, 1000},   // ¥1000 (zero-decimal currency)
		{"zero amount", 0, stripe.CurrencyUSD, 0},             // $0.00
		{"negative refund", -2500, stripe.CurrencyUSD, -2500}, // -$25.00 (credit memo)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stripeInvoice := &stripe.Invoice{
				ID: "in_amount",
				Customer: &stripe.Customer{
					ID: "cus_test",
				},
				Total:    tt.total,
				Currency: tt.currency,
				Status:   stripe.InvoiceStatusPaid,
				Created:  1609459200,
			}

			result := SanitizeInvoice(stripeInvoice)

			if result.Total != tt.expected {
				t.Errorf("Expected Total %d, got %d", tt.expected, result.Total)
			}

			if result.Currency != string(tt.currency) {
				t.Errorf("Expected Currency '%s', got '%s'", tt.currency, result.Currency)
			}
		})
	}
}

// TestSanitizeInvoice_NoPIILeakage verifies that PII fields are not copied.
func TestSanitizeInvoice_NoPIILeakage(t *testing.T) {
	// Build an invoice with PII fields populated
	stripeInvoice := &stripe.Invoice{
		ID: "in_nopii",
		Customer: &stripe.Customer{
			ID:    "cus_nopii",
			Email: "customer@example.com", // PII
			Name:  "Jane Smith",           // PII
		},
		Total:    10000,
		Currency: stripe.CurrencyUSD,
		Status:   stripe.InvoiceStatusPaid,
		Created:  1609459200,
	}

	result := SanitizeInvoice(stripeInvoice)

	// The result should only have the customer ID, not the full customer object
	if result.Customer != models.SynStripeID("cus_nopii") {
		t.Errorf("Expected only customer ID, got %+v", result.Customer)
	}

	// The models.Invoice struct has no fields for email, name, billing details, etc.
	// If we were to add them, this test would fail at compile time.
}

// TestSanitizeInvoice_TimestampMapping verifies that Unix timestamps are preserved correctly.
func TestSanitizeInvoice_TimestampMapping(t *testing.T) {
	testTimestamp := int64(1640995200) // 2022-01-01 00:00:00 UTC

	stripeInvoice := &stripe.Invoice{
		ID: "in_time",
		Customer: &stripe.Customer{
			ID: "cus_test",
		},
		Total:    1000,
		Currency: stripe.CurrencyUSD,
		Status:   stripe.InvoiceStatusPaid,
		Created:  testTimestamp,
	}

	result := SanitizeInvoice(stripeInvoice)

	if result.CreatedAt != testTimestamp {
		t.Errorf("Expected CreatedAt %d, got %d", testTimestamp, result.CreatedAt)
	}
}

// TestSanitizeSubscription_TimestampMapping verifies that Unix timestamps are preserved correctly.
func TestSanitizeSubscription_TimestampMapping(t *testing.T) {
	testTimestamp := int64(1640995200) // 2022-01-01 00:00:00 UTC

	stripeSubscription := &stripe.Subscription{
		ID: "sub_time",
		Customer: &stripe.Customer{
			ID: "cus_test",
		},
		Status:  stripe.SubscriptionStatusActive,
		Created: testTimestamp,
		Items: &stripe.SubscriptionItemList{
			Data: []*stripe.SubscriptionItem{
				{
					Price: &stripe.Price{
						ID: "price_test",
						Product: &stripe.Product{
							ID: "prod_test",
						},
					},
					Quantity: 1,
				},
			},
		},
	}

	result := SanitizeSubscription(stripeSubscription)

	if result.CreatedAt != testTimestamp {
		t.Errorf("Expected CreatedAt %d, got %d", testTimestamp, result.CreatedAt)
	}
}
