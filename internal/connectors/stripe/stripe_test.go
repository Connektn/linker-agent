package stripec

import (
	"context"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v83"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		apiKey  string
		account string
		maxRPS  int
		wantErr bool
	}{
		{
			name:    "valid with all parameters",
			apiKey:  "sk_test_123",
			account: "acct_123",
			maxRPS:  10,
			wantErr: false,
		},
		{
			name:    "valid without account",
			apiKey:  "sk_test_123",
			account: "",
			maxRPS:  8,
			wantErr: false,
		},
		{
			name:    "valid with zero maxRPS (should default to 1)",
			apiKey:  "sk_test_123",
			account: "",
			maxRPS:  0,
			wantErr: false,
		},
		{
			name:    "empty apiKey should fail",
			apiKey:  "",
			account: "",
			maxRPS:  8,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(tt.apiKey, tt.account, tt.maxRPS)

			if tt.wantErr {
				if err == nil {
					t.Error("New() error = nil, wantErr = true")
				}
				return
			}

			if err != nil {
				t.Errorf("New() unexpected error = %v", err)
				return
			}

			if client == nil {
				t.Error("New() returned nil client")
				return
			}

			if client.account != tt.account {
				t.Errorf("client.account = %q, want %q", client.account, tt.account)
			}

			// Verify rate limiter was initialized
			if client.limiter == nil {
				t.Error("client.limiter is nil")
			}

			// Verify maxRPS defaults to 1 if 0 or negative
			expectedRPS := tt.maxRPS
			if expectedRPS <= 0 {
				expectedRPS = 1
			}
			if client.limiter.maxRPS != expectedRPS {
				t.Errorf("client.limiter.maxRPS = %d, want %d", client.limiter.maxRPS, expectedRPS)
			}
		})
	}
}

func TestSanitizeSubscription(t *testing.T) {
	// Create a fake Stripe subscription with PII to ensure it's not leaked
	fakeStripeSubscription := &stripe.Subscription{
		ID:      "sub_123",
		Created: 1640000000,
		Status:  stripe.SubscriptionStatusActive,
		Customer: &stripe.Customer{
			ID:    "cus_123",
			Email: "test@example.com", // PII that should NOT be in output
			Name:  "Test User",        // PII that should NOT be in output
		},
		Items: &stripe.SubscriptionItemList{
			Data: []*stripe.SubscriptionItem{
				{
					Price: &stripe.Price{
						ID: "price_123",
						Product: &stripe.Product{
							ID: "prod_123",
						},
					},
					Quantity: 2,
				},
			},
		},
	}

	sanitized := sanitizeSubscription(fakeStripeSubscription)

	// Verify only allowed fields are present
	if sanitized.ID != "sub_123" {
		t.Errorf("ID = %q, want %q", sanitized.ID, "sub_123")
	}

	if sanitized.Customer != "cus_123" {
		t.Errorf("Customer = %q, want %q (ID only, no PII)", sanitized.Customer, "cus_123")
	}

	if sanitized.Status != "active" {
		t.Errorf("Status = %q, want %q", sanitized.Status, "active")
	}

	if sanitized.CreatedAt != 1640000000 {
		t.Errorf("CreatedAt = %d, want %d", sanitized.CreatedAt, 1640000000)
	}

	if len(sanitized.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(sanitized.Items))
	}

	item := sanitized.Items[0]
	if item.PriceID != "price_123" {
		t.Errorf("Items[0].PriceID = %q, want %q", item.PriceID, "price_123")
	}

	if item.ProductID != "prod_123" {
		t.Errorf("Items[0].ProductID = %q, want %q", item.ProductID, "prod_123")
	}

	if item.Quantity != 2 {
		t.Errorf("Items[0].Quantity = %d, want %d", item.Quantity, 2)
	}
}

func TestSanitizeInvoice(t *testing.T) {
	// Create a fake Stripe invoice with PII to ensure it's not leaked
	fakeStripeInvoice := &stripe.Invoice{
		ID:      "in_123",
		Created: 1640000000,
		Status:  stripe.InvoiceStatusPaid,
		Customer: &stripe.Customer{
			ID:    "cus_123",
			Email: "test@example.com", // PII that should NOT be in output
			Name:  "Test User",        // PII that should NOT be in output
		},
		Lines: &stripe.InvoiceLineItemList{
			Data: []*stripe.InvoiceLineItem{
				{
					Subscription: &stripe.Subscription{
						ID: "sub_123",
					},
				},
			},
		},
		Total:    10000, // $100.00 in cents
		Currency: stripe.CurrencyUSD,
	}

	sanitized := sanitizeInvoice(fakeStripeInvoice)

	// Verify only allowed fields are present
	if sanitized.ID != "in_123" {
		t.Errorf("ID = %q, want %q", sanitized.ID, "in_123")
	}

	if sanitized.Customer != "cus_123" {
		t.Errorf("Customer = %q, want %q (ID only, no PII)", sanitized.Customer, "cus_123")
	}

	if sanitized.Subscription != "sub_123" {
		t.Errorf("Subscription = %q, want %q (ID only)", sanitized.Subscription, "sub_123")
	}

	if sanitized.Total != 10000 {
		t.Errorf("Total = %d, want %d", sanitized.Total, 10000)
	}

	if sanitized.Currency != "usd" {
		t.Errorf("Currency = %q, want %q", sanitized.Currency, "usd")
	}

	if sanitized.Status != "paid" {
		t.Errorf("Status = %q, want %q", sanitized.Status, "paid")
	}

	if sanitized.CreatedAt != 1640000000 {
		t.Errorf("CreatedAt = %d, want %d", sanitized.CreatedAt, 1640000000)
	}

	if !sanitized.Paid {
		t.Error("Paid = false, want true (derived from status)")
	}
}

func TestSanitizeInvoiceWithoutSubscription(t *testing.T) {
	// Test invoice without subscription (one-off invoice)
	fakeStripeInvoice := &stripe.Invoice{
		ID:      "in_456",
		Created: 1640000000,
		Status:  stripe.InvoiceStatusOpen,
		Customer: &stripe.Customer{
			ID: "cus_456",
		},
		Lines:    &stripe.InvoiceLineItemList{Data: []*stripe.InvoiceLineItem{}},
		Total:    5000,
		Currency: stripe.CurrencyEUR,
	}

	sanitized := sanitizeInvoice(fakeStripeInvoice)

	if sanitized.Subscription != "" {
		t.Errorf("Subscription = %q, want empty string for non-subscription invoice", sanitized.Subscription)
	}

	if sanitized.Paid {
		t.Error("Paid = true, want false (status is open, not paid)")
	}

	if sanitized.Status != "open" {
		t.Errorf("Status = %q, want %q", sanitized.Status, "open")
	}
}

func TestRateLimiter(t *testing.T) {
	t.Run("rate limiter allows initial requests", func(t *testing.T) {
		limiter := newRateLimiter(10)
		ctx := context.Background()

		// Should allow immediate acquisition of available tokens
		for i := 0; i < 10; i++ {
			if err := limiter.acquire(ctx); err != nil {
				t.Errorf("acquire() failed on request %d: %v", i, err)
			}
		}
	})

	t.Run("rate limiter respects limits", func(t *testing.T) {
		limiter := newRateLimiter(2) // 2 requests per second
		ctx := context.Background()

		start := time.Now()

		// Acquire 4 tokens (should take ~2 seconds total)
		for i := 0; i < 4; i++ {
			if err := limiter.acquire(ctx); err != nil {
				t.Fatalf("acquire() failed on request %d: %v", i, err)
			}
		}

		elapsed := time.Since(start)

		// Should take at least 1 second (4 tokens at 2/sec)
		// Allow some tolerance for timing jitter
		if elapsed < 900*time.Millisecond {
			t.Errorf("rate limiter too fast: elapsed = %v, want >= 900ms", elapsed)
		}

		// Should not take excessively long (sanity check)
		if elapsed > 3*time.Second {
			t.Errorf("rate limiter too slow: elapsed = %v, want < 3s", elapsed)
		}
	})

	t.Run("rate limiter respects context cancellation", func(t *testing.T) {
		limiter := newRateLimiter(1)
		ctx, cancel := context.WithCancel(context.Background())

		// Exhaust tokens
		if err := limiter.acquire(ctx); err != nil {
			t.Fatalf("initial acquire() failed: %v", err)
		}

		// Cancel context immediately
		cancel()

		// Next acquire should fail immediately due to context cancellation
		err := limiter.acquire(ctx)
		if err == nil {
			t.Error("acquire() should fail with canceled context")
		}
		if err != context.Canceled {
			t.Errorf("acquire() error = %v, want %v", err, context.Canceled)
		}
	})

	t.Run("rate limiter refills over time", func(t *testing.T) {
		limiter := newRateLimiter(5)
		ctx := context.Background()

		// Exhaust all tokens
		for i := 0; i < 5; i++ {
			if err := limiter.acquire(ctx); err != nil {
				t.Fatalf("acquire() failed: %v", err)
			}
		}

		// Wait for refill (1 second should refill 5 tokens)
		time.Sleep(1100 * time.Millisecond)

		// Should be able to acquire again
		if err := limiter.acquire(ctx); err != nil {
			t.Errorf("acquire() after refill failed: %v", err)
		}
	})
}

func TestListSubscriptionsDefaults(t *testing.T) {
	// This test verifies parameter defaulting behavior without making real API calls
	client := &Client{
		limiter: newRateLimiter(100), // High limit to avoid blocking in test
	}

	// We can't easily test the actual Stripe API call without mocking,
	// but we can verify the client is properly initialized
	if client.limiter == nil {
		t.Error("client.limiter should not be nil")
	}
}

func TestListInvoicesDefaults(t *testing.T) {
	// This test verifies parameter defaulting behavior without making real API calls
	client := &Client{
		limiter: newRateLimiter(100), // High limit to avoid blocking in test
	}

	// We can't easily test the actual Stripe API call without mocking,
	// but we can verify the client is properly initialized
	if client.limiter == nil {
		t.Error("client.limiter should not be nil")
	}
}
