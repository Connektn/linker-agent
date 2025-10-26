package stripec

import (
	"strings"
	"testing"

	"github.com/stripe/stripe-go/v83"
)

var testSalt = []byte("test_salt_for_synthetic_ids")

// TestSanitizeSubscription_SyntheticIDs verifies that all IDs are converted to synthetic IDs.
func TestSanitizeSubscription_SyntheticIDs(t *testing.T) {
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

	result, err := SanitizeSubscription(testSalt, stripeSubscription)
	if err != nil {
		t.Fatalf("SanitizeSubscription failed: %v", err)
	}

	// Verify synthetic ID prefixes
	if !strings.HasPrefix(result.ID, "syn_sub_") {
		t.Errorf("Expected ID to start with 'syn_sub_', got '%s'", result.ID)
	}

	if !strings.HasPrefix(string(result.Customer), "syn_cust_") {
		t.Errorf("Expected Customer to start with 'syn_cust_', got '%s'", result.Customer)
	}

	// Verify no raw Stripe IDs are present
	if result.ID == "sub_test123" {
		t.Errorf("Raw subscription ID leaked: %s", result.ID)
	}

	if string(result.Customer) == "cus_test456" {
		t.Errorf("Raw customer ID leaked: %s", result.Customer)
	}

	// Verify status is preserved (enum value, not an ID)
	if result.Status != "active" {
		t.Errorf("Expected Status 'active', got '%s'", result.Status)
	}

	if result.CreatedAt != 1609459200 {
		t.Errorf("Expected CreatedAt 1609459200, got %d", result.CreatedAt)
	}

	if len(result.Items) != 1 {
		t.Fatalf("Expected 1 item, got %d", len(result.Items))
	}

	// Verify item IDs are synthetic
	item := result.Items[0]
	if !strings.HasPrefix(item.PriceID, "syn_price_") {
		t.Errorf("Expected PriceID to start with 'syn_price_', got '%s'", item.PriceID)
	}

	if !strings.HasPrefix(item.ProductID, "syn_prod_") {
		t.Errorf("Expected ProductID to start with 'syn_prod_', got '%s'", item.ProductID)
	}

	// Verify no raw IDs in items
	if item.PriceID == "price_test789" {
		t.Errorf("Raw price ID leaked: %s", item.PriceID)
	}

	if item.ProductID == "prod_testABC" {
		t.Errorf("Raw product ID leaked: %s", item.ProductID)
	}

	if item.Quantity != 2 {
		t.Errorf("Expected Quantity 2, got %d", item.Quantity)
	}
}

// TestSanitizeSubscription_Deterministic verifies that synthetic IDs are deterministic.
func TestSanitizeSubscription_Deterministic(t *testing.T) {
	stripeSubscription := &stripe.Subscription{
		ID: "sub_deterministic",
		Customer: &stripe.Customer{
			ID: "cus_deterministic",
		},
		Status:  stripe.SubscriptionStatusActive,
		Created: 1609459200,
		Items: &stripe.SubscriptionItemList{
			Data: []*stripe.SubscriptionItem{
				{
					Price: &stripe.Price{
						ID: "price_det",
						Product: &stripe.Product{
							ID: "prod_det",
						},
					},
					Quantity: 1,
				},
			},
		},
	}

	// Call twice with same salt
	result1, err1 := SanitizeSubscription(testSalt, stripeSubscription)
	if err1 != nil {
		t.Fatalf("First SanitizeSubscription failed: %v", err1)
	}

	result2, err2 := SanitizeSubscription(testSalt, stripeSubscription)
	if err2 != nil {
		t.Fatalf("Second SanitizeSubscription failed: %v", err2)
	}

	// Verify deterministic output
	if result1.ID != result2.ID {
		t.Errorf("Subscription IDs not deterministic: %s != %s", result1.ID, result2.ID)
	}

	if result1.Customer != result2.Customer {
		t.Errorf("Customer IDs not deterministic: %s != %s", result1.Customer, result2.Customer)
	}

	if len(result1.Items) > 0 && len(result2.Items) > 0 {
		if result1.Items[0].PriceID != result2.Items[0].PriceID {
			t.Errorf("Price IDs not deterministic: %s != %s", result1.Items[0].PriceID, result2.Items[0].PriceID)
		}
		if result1.Items[0].ProductID != result2.Items[0].ProductID {
			t.Errorf("Product IDs not deterministic: %s != %s", result1.Items[0].ProductID, result2.Items[0].ProductID)
		}
	}
}

// TestSanitizeInvoice_SyntheticIDs verifies that all invoice IDs are converted to synthetic IDs.
func TestSanitizeInvoice_SyntheticIDs(t *testing.T) {
	stripeInvoice := &stripe.Invoice{
		ID: "in_test123",
		Customer: &stripe.Customer{
			ID: "cus_test456",
		},
		Total:    10000,
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

	result, err := SanitizeInvoice(testSalt, stripeInvoice)
	if err != nil {
		t.Fatalf("SanitizeInvoice failed: %v", err)
	}

	// Verify synthetic ID prefixes
	if !strings.HasPrefix(result.ID, "syn_inv_") {
		t.Errorf("Expected ID to start with 'syn_inv_', got '%s'", result.ID)
	}

	if !strings.HasPrefix(string(result.Customer), "syn_cust_") {
		t.Errorf("Expected Customer to start with 'syn_cust_', got '%s'", result.Customer)
	}

	if !strings.HasPrefix(result.Subscription, "syn_sub_") {
		t.Errorf("Expected Subscription to start with 'syn_sub_', got '%s'", result.Subscription)
	}

	// Verify no raw Stripe IDs are present
	if result.ID == "in_test123" {
		t.Errorf("Raw invoice ID leaked: %s", result.ID)
	}

	if string(result.Customer) == "cus_test456" {
		t.Errorf("Raw customer ID leaked: %s", result.Customer)
	}

	if result.Subscription == "sub_test789" {
		t.Errorf("Raw subscription ID leaked: %s", result.Subscription)
	}

	// Verify non-ID fields are preserved
	if result.Total != 10000 {
		t.Errorf("Expected Total 10000, got %d", result.Total)
	}

	if result.Currency != "usd" {
		t.Errorf("Expected Currency 'usd', got '%s'", result.Currency)
	}

	if result.Status != "paid" {
		t.Errorf("Expected Status 'paid', got '%s'", result.Status)
	}

	if !result.Paid {
		t.Errorf("Expected Paid true, got false")
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

	result, err := SanitizeInvoice(testSalt, stripeInvoice)
	if err != nil {
		t.Fatalf("SanitizeInvoice failed: %v", err)
	}

	if result.Subscription != "" {
		t.Errorf("Expected empty Subscription, got '%s'", result.Subscription)
	}

	// Still verify other IDs are synthetic
	if !strings.HasPrefix(result.ID, "syn_inv_") {
		t.Errorf("Expected ID to start with 'syn_inv_', got '%s'", result.ID)
	}

	if !strings.HasPrefix(string(result.Customer), "syn_cust_") {
		t.Errorf("Expected Customer to start with 'syn_cust_', got '%s'", result.Customer)
	}
}

// TestSanitizeInvoice_Deterministic verifies that synthetic IDs are deterministic.
func TestSanitizeInvoice_Deterministic(t *testing.T) {
	stripeInvoice := &stripe.Invoice{
		ID: "in_deterministic",
		Customer: &stripe.Customer{
			ID: "cus_deterministic",
		},
		Total:    10000,
		Currency: stripe.CurrencyUSD,
		Status:   stripe.InvoiceStatusPaid,
		Created:  1609459200,
		Lines: &stripe.InvoiceLineItemList{
			Data: []*stripe.InvoiceLineItem{
				{
					Subscription: &stripe.Subscription{
						ID: "sub_deterministic",
					},
				},
			},
		},
	}

	// Call twice with same salt
	result1, err1 := SanitizeInvoice(testSalt, stripeInvoice)
	if err1 != nil {
		t.Fatalf("First SanitizeInvoice failed: %v", err1)
	}

	result2, err2 := SanitizeInvoice(testSalt, stripeInvoice)
	if err2 != nil {
		t.Fatalf("Second SanitizeInvoice failed: %v", err2)
	}

	// Verify deterministic output
	if result1.ID != result2.ID {
		t.Errorf("Invoice IDs not deterministic: %s != %s", result1.ID, result2.ID)
	}

	if result1.Customer != result2.Customer {
		t.Errorf("Customer IDs not deterministic: %s != %s", result1.Customer, result2.Customer)
	}

	if result1.Subscription != result2.Subscription {
		t.Errorf("Subscription IDs not deterministic: %s != %s", result1.Subscription, result2.Subscription)
	}
}

// TestSanitize_DifferentSalts verifies that different salts produce different synthetic IDs.
func TestSanitize_DifferentSalts(t *testing.T) {
	salt1 := []byte("salt_one")
	salt2 := []byte("salt_two")

	stripeSubscription := &stripe.Subscription{
		ID: "sub_multi_tenant",
		Customer: &stripe.Customer{
			ID: "cus_shared",
		},
		Status:  stripe.SubscriptionStatusActive,
		Created: 1609459200,
		Items:   &stripe.SubscriptionItemList{Data: []*stripe.SubscriptionItem{}},
	}

	result1, err1 := SanitizeSubscription(salt1, stripeSubscription)
	if err1 != nil {
		t.Fatalf("SanitizeSubscription with salt1 failed: %v", err1)
	}

	result2, err2 := SanitizeSubscription(salt2, stripeSubscription)
	if err2 != nil {
		t.Fatalf("SanitizeSubscription with salt2 failed: %v", err2)
	}

	// Verify different salts produce different synthetic IDs
	if result1.ID == result2.ID {
		t.Errorf("Different salts produced same subscription ID: %s", result1.ID)
	}

	if result1.Customer == result2.Customer {
		t.Errorf("Different salts produced same customer ID: %s", result1.Customer)
	}
}
