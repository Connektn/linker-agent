// Package stripec provides a minimal, privacy-safe Stripe API connector for
// the Connektn Linker Agent. It uses read-only Stripe APIs and returns only
// sanitized data with no PII (Personal Identifiable Information).
//
// Security: This connector never logs or returns customer email, name, address,
// phone, or any free-form text fields. All identifiers are treated as opaque
// strings and never parsed or generated.
//
// Threat model: The connector assumes API keys are properly secured and rotated.
// Rate limiting is basic and in-memory; production deployments should monitor
// API usage via Stripe's dashboard.
package stripec

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/stripe/stripe-go/v83"
)

// Client is a privacy-safe Stripe API client that provides read-only access
// to subscriptions and invoices without exposing PII.
type Client struct {
	api     *stripe.Client
	account string // optional Stripe Connect account ID
	limiter *rateLimiter
}

// Subscription represents a sanitized Stripe subscription with no PII.
type Subscription struct {
	ID        string    // Stripe subscription ID
	Customer  string    // Stripe customer ID (opaque, no PII)
	Status    string    // subscription status (active, canceled, etc.)
	Items     []SubItem // subscription items (price/product IDs and quantities)
	CreatedAt int64     // Unix timestamp in seconds
}

// SubItem represents a sanitized subscription item with price and product information.
type SubItem struct {
	PriceID   string // Stripe price ID
	ProductID string // Stripe product ID
	Quantity  int64  // quantity of this item
}

// Invoice represents a sanitized Stripe invoice with no PII.
type Invoice struct {
	ID           string // Stripe invoice ID
	Customer     string // Stripe customer ID (opaque, no PII)
	Subscription string // Stripe subscription ID (opaque)
	Total        int64  // total amount in currency minor units (e.g., cents)
	Currency     string // three-letter currency code (e.g., "usd")
	Status       string // invoice status (draft, open, paid, void, uncollectible)
	CreatedAt    int64  // Unix timestamp in seconds
	Paid         bool   // whether the invoice has been paid
}

// New creates a new Stripe client with the given API key, optional Connect account,
// and rate limiting configuration.
//
// Parameters:
//   - apiKey: Stripe API key (required, must be non-empty)
//   - account: optional Stripe Connect account ID (empty string if not using Connect)
//   - maxRPS: maximum requests per second (must be > 0, used for rate limiting)
//
// Returns an error if apiKey is empty.
func New(apiKey, account string, maxRPS int) (*Client, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("stripe apiKey must be non-empty")
	}

	// Ensure maxRPS is at least 1
	if maxRPS <= 0 {
		maxRPS = 1
	}

	// Initialize the Stripe API client
	sc := stripe.NewClient(apiKey, nil)

	return &Client{
		api:     sc,
		account: account,
		limiter: newRateLimiter(maxRPS),
	}, nil
}

// SmokeCheck performs a minimal read operation to verify API credentials.
// It lists up to 5 customers and returns the count without exposing any PII.
//
// This is useful for validating configuration during startup or debugging.
func (c *Client) SmokeCheck(ctx context.Context) (int, error) {
	// Apply rate limiting
	if err := c.limiter.acquire(ctx); err != nil {
		return 0, fmt.Errorf("stripe smoke check rate limit: %w", err)
	}

	params := &stripe.CustomerListParams{
		ListParams: stripe.ListParams{
			Limit: stripe.Int64(5),
		},
	}

	// Set Stripe Connect account if specified
	if c.account != "" {
		params.SetStripeAccount(c.account)
	}

	count := 0
	var listErr error

	// Use the V1 API with Seq2 iterator pattern
	c.api.V1Customers.List(ctx, params)(func(customer *stripe.Customer, err error) bool {
		if err != nil {
			listErr = err
			return false // stop iteration on error
		}
		count++
		return true // continue iteration
	})

	if listErr != nil {
		return 0, fmt.Errorf("stripe smoke check: %w", listErr)
	}

	return count, nil
}

// ListSubscriptions retrieves a list of subscriptions, optionally filtered by customer ID.
// Returns sanitized subscription data with no PII.
//
// Parameters:
//   - ctx: context for cancellation
//   - customerID: optional customer ID to filter by (empty string for all)
//   - limit: maximum number of subscriptions to return (0 defaults to 10)
//
// Returns an empty slice on success with no results, never nil.
func (c *Client) ListSubscriptions(ctx context.Context, customerID string, limit int64) ([]Subscription, error) {
	// Apply rate limiting
	if err := c.limiter.acquire(ctx); err != nil {
		return nil, fmt.Errorf("stripe list subscriptions rate limit: %w", err)
	}

	// Default limit to 10 if not specified
	if limit <= 0 {
		limit = 10
	}

	params := &stripe.SubscriptionListParams{
		ListParams: stripe.ListParams{
			Limit: stripe.Int64(limit),
		},
	}

	// Filter by customer if specified
	if customerID != "" {
		params.Customer = stripe.String(customerID)
	}

	// Set Stripe Connect account if specified
	if c.account != "" {
		params.SetStripeAccount(c.account)
	}

	var result []Subscription
	var listErr error

	// Use the V1 API with Seq2 iterator pattern
	c.api.V1Subscriptions.List(ctx, params)(func(sub *stripe.Subscription, err error) bool {
		if err != nil {
			listErr = err
			return false // stop iteration on error
		}
		result = append(result, sanitizeSubscription(sub))
		return true // continue iteration
	})

	if listErr != nil {
		return nil, fmt.Errorf("stripe list subscriptions: %w", listErr)
	}

	// Return empty slice instead of nil
	if result == nil {
		result = []Subscription{}
	}

	return result, nil
}

// ListRawSubscriptions retrieves raw Stripe subscription objects for sanitization with synthetic IDs.
// This method returns the full Stripe API response objects without basic sanitization,
// allowing the caller to apply tenant-scoped synthetic ID generation.
//
// Parameters:
//   - ctx: context for cancellation
//   - customerID: optional customer ID to filter by (empty string for all)
//   - limit: maximum number of subscriptions to return (0 defaults to 10)
//
// Returns an empty slice on success with no results, never nil.
func (c *Client) ListRawSubscriptions(ctx context.Context, customerID string, limit int64) ([]*stripe.Subscription, error) {
	// Apply rate limiting
	if err := c.limiter.acquire(ctx); err != nil {
		return nil, fmt.Errorf("stripe list raw subscriptions rate limit: %w", err)
	}

	// Default limit to 10 if not specified
	if limit <= 0 {
		limit = 10
	}

	params := &stripe.SubscriptionListParams{
		ListParams: stripe.ListParams{
			Limit: stripe.Int64(limit),
		},
	}

	// Filter by customer if specified
	if customerID != "" {
		params.Customer = stripe.String(customerID)
	}

	// Set Stripe Connect account if specified
	if c.account != "" {
		params.SetStripeAccount(c.account)
	}

	var result []*stripe.Subscription
	var listErr error

	// Use the V1 API with Seq2 iterator pattern
	c.api.V1Subscriptions.List(ctx, params)(func(sub *stripe.Subscription, err error) bool {
		if err != nil {
			listErr = err
			return false // stop iteration on error
		}
		result = append(result, sub)
		return true // continue iteration
	})

	if listErr != nil {
		return nil, fmt.Errorf("stripe list raw subscriptions: %w", listErr)
	}

	// Return empty slice instead of nil
	if result == nil {
		result = []*stripe.Subscription{}
	}

	return result, nil
}

// ListInvoices retrieves a list of invoices, optionally filtered by customer ID
// and/or subscription ID. Returns sanitized invoice data with no PII.
//
// Parameters:
//   - ctx: context for cancellation
//   - customerID: optional customer ID to filter by (empty string for all)
//   - subscriptionID: optional subscription ID to filter by (empty string for all)
//   - limit: maximum number of invoices to return (0 defaults to 10)
//
// Returns an empty slice on success with no results, never nil.
func (c *Client) ListInvoices(ctx context.Context, customerID, subscriptionID string, limit int64) ([]Invoice, error) {
	// Apply rate limiting
	if err := c.limiter.acquire(ctx); err != nil {
		return nil, fmt.Errorf("stripe list invoices rate limit: %w", err)
	}

	// Default limit to 10 if not specified
	if limit <= 0 {
		limit = 10
	}

	params := &stripe.InvoiceListParams{
		ListParams: stripe.ListParams{
			Limit: stripe.Int64(limit),
		},
	}

	// Filter by customer if specified
	if customerID != "" {
		params.Customer = stripe.String(customerID)
	}

	// Filter by subscription if specified
	if subscriptionID != "" {
		params.Subscription = stripe.String(subscriptionID)
	}

	// Set Stripe Connect account if specified
	if c.account != "" {
		params.SetStripeAccount(c.account)
	}

	var result []Invoice
	var listErr error

	// Use the V1 API with Seq2 iterator pattern
	c.api.V1Invoices.List(ctx, params)(func(inv *stripe.Invoice, err error) bool {
		if err != nil {
			listErr = err
			return false // stop iteration on error
		}
		result = append(result, sanitizeInvoice(inv))
		return true // continue iteration
	})

	if listErr != nil {
		return nil, fmt.Errorf("stripe list invoices: %w", listErr)
	}

	// Return empty slice instead of nil
	if result == nil {
		result = []Invoice{}
	}

	return result, nil
}

// ListRawInvoices retrieves raw Stripe invoice objects for sanitization with synthetic IDs.
// This method returns the full Stripe API response objects without basic sanitization,
// allowing the caller to apply tenant-scoped synthetic ID generation.
//
// Parameters:
//   - ctx: context for cancellation
//   - customerID: optional customer ID to filter by (empty string for all)
//   - subscriptionID: optional subscription ID to filter by (empty string for all)
//   - limit: maximum number of invoices to return (0 defaults to 10)
//
// Returns an empty slice on success with no results, never nil.
func (c *Client) ListRawInvoices(ctx context.Context, customerID, subscriptionID string, limit int64) ([]*stripe.Invoice, error) {
	// Apply rate limiting
	if err := c.limiter.acquire(ctx); err != nil {
		return nil, fmt.Errorf("stripe list raw invoices rate limit: %w", err)
	}

	// Default limit to 10 if not specified
	if limit <= 0 {
		limit = 10
	}

	params := &stripe.InvoiceListParams{
		ListParams: stripe.ListParams{
			Limit: stripe.Int64(limit),
		},
	}

	// Filter by customer if specified
	if customerID != "" {
		params.Customer = stripe.String(customerID)
	}

	// Filter by subscription if specified
	if subscriptionID != "" {
		params.Subscription = stripe.String(subscriptionID)
	}

	// Set Stripe Connect account if specified
	if c.account != "" {
		params.SetStripeAccount(c.account)
	}

	var result []*stripe.Invoice
	var listErr error

	// Use the V1 API with Seq2 iterator pattern
	c.api.V1Invoices.List(ctx, params)(func(inv *stripe.Invoice, err error) bool {
		if err != nil {
			listErr = err
			return false // stop iteration on error
		}
		result = append(result, inv)
		return true // continue iteration
	})

	if listErr != nil {
		return nil, fmt.Errorf("stripe list raw invoices: %w", listErr)
	}

	// Return empty slice instead of nil
	if result == nil {
		result = []*stripe.Invoice{}
	}

	return result, nil
}

// sanitizeSubscription maps a Stripe subscription to our sanitized model,
// extracting only non-PII fields (IDs, status, timestamps, quantities).
//
// Privacy: This function explicitly omits customer objects, metadata,
// and any fields that could contain PII.
func sanitizeSubscription(s *stripe.Subscription) Subscription {
	var items []SubItem
	for _, item := range s.Items.Data {
		items = append(items, SubItem{
			PriceID:   item.Price.ID,
			ProductID: item.Price.Product.ID,
			Quantity:  item.Quantity,
		})
	}

	return Subscription{
		ID:        s.ID,
		Customer:  s.Customer.ID,
		Status:    string(s.Status),
		Items:     items,
		CreatedAt: s.Created,
	}
}

// sanitizeInvoice maps a Stripe invoice to our sanitized model,
// extracting only non-PII fields (IDs, amounts, currency, status, timestamps).
//
// Privacy: This function explicitly omits customer objects, billing details,
// metadata, and any fields that could contain PII.
func sanitizeInvoice(i *stripe.Invoice) Invoice {
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

	return Invoice{
		ID:           i.ID,
		Customer:     i.Customer.ID,
		Subscription: subscriptionID,
		Total:        i.Total,
		Currency:     string(i.Currency),
		Status:       string(i.Status),
		CreatedAt:    i.Created,
		Paid:         paid,
	}
}

// rateLimiter implements a simple token bucket rate limiter for API calls.
// This is a minimal, in-memory implementation suitable for single-instance deployments.
type rateLimiter struct {
	maxRPS   int
	tokens   int
	mu       sync.Mutex
	lastFill time.Time
}

// newRateLimiter creates a new rate limiter with the specified requests per second.
func newRateLimiter(maxRPS int) *rateLimiter {
	return &rateLimiter{
		maxRPS:   maxRPS,
		tokens:   maxRPS,
		lastFill: time.Now(),
	}
}

// acquire attempts to acquire a token from the rate limiter.
// It blocks briefly (≤50ms) if tokens are unavailable, respecting context cancellation.
//
// Returns an error if the context is canceled before a token becomes available.
func (rl *rateLimiter) acquire(ctx context.Context) error {
	const maxSleep = 50 * time.Millisecond

	for {
		rl.mu.Lock()

		// Refill tokens based on elapsed time
		now := time.Now()
		elapsed := now.Sub(rl.lastFill)
		tokensToAdd := int(elapsed.Seconds() * float64(rl.maxRPS))

		if tokensToAdd > 0 {
			rl.tokens += tokensToAdd
			if rl.tokens > rl.maxRPS {
				rl.tokens = rl.maxRPS
			}
			rl.lastFill = now
		}

		// Try to acquire a token
		if rl.tokens > 0 {
			rl.tokens--
			rl.mu.Unlock()
			return nil
		}

		rl.mu.Unlock()

		// Wait briefly before retrying, respecting context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(maxSleep):
			// Continue loop to retry
		}
	}
}
