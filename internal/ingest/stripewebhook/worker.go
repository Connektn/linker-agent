package stripewebhook

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v83"
	"github.com/stripe/stripe-go/v83/invoice"
	"github.com/stripe/stripe-go/v83/subscription"
	"linker-agent/internal/config"
	stripec "linker-agent/internal/connectors/stripe"
	"linker-agent/internal/exporter"
	"linker-agent/internal/matchers"
	"linker-agent/internal/pipeline"
)

// SupportedEventTypes lists the webhook event types we process.
var SupportedEventTypes = map[string]bool{
	"invoice.created":                        true,
	"invoice.finalized":                      true,
	"invoice.payment_succeeded":              true,
	"invoice.payment_failed":                 true,
	"customer.subscription.created":          true,
	"customer.subscription.updated":          true,
	"customer.subscription.deleted":          true,
	"charge.succeeded":                       true,
	"charge.refunded":                        true,
}

// Worker processes webhook jobs by fetching canonical objects from Stripe,
// sanitizing them, running the matcher pipeline, and exporting edges.
type Worker struct {
	stripeClient   *stripe.Client
	invoiceClient  invoice.Client
	subClient      subscription.Client
	account        string // Stripe Connect account (optional)
	tenantSalt     []byte
	ensemble       matchers.Ensemble
	exporter       *exporter.Exporter
	retryCfg       config.StripeWebhookRetry
	logger         *slog.Logger
}

// WorkerOptions configures the webhook worker.
type WorkerOptions struct {
	StripeAPIKey string
	StripeAccount string // Optional Stripe Connect account
	TenantSalt   []byte
	Ensemble     matchers.Ensemble
	Exporter     *exporter.Exporter
	RetryConfig  config.StripeWebhookRetry
	Logger       *slog.Logger
}

// NewWorker creates a new webhook worker.
func NewWorker(opts WorkerOptions) (*Worker, error) {
	if opts.StripeAPIKey == "" {
		return nil, fmt.Errorf("webhook worker: stripe API key is required")
	}

	if len(opts.TenantSalt) == 0 {
		return nil, fmt.Errorf("webhook worker: tenant salt is required")
	}

	if opts.Exporter == nil {
		return nil, fmt.Errorf("webhook worker: exporter is required")
	}

	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	// Create Stripe client
	stripeClient := stripe.NewClient(opts.StripeAPIKey, nil)

	// Create backend clients for invoices and subscriptions
	// These clients use their own backend with the API key
	backend := stripe.GetBackend(stripe.APIBackend)
	invoiceClient := invoice.Client{
		B:   backend,
		Key: opts.StripeAPIKey,
	}
	subClient := subscription.Client{
		B:   backend,
		Key: opts.StripeAPIKey,
	}

	return &Worker{
		stripeClient:   stripeClient,
		invoiceClient:  invoiceClient,
		subClient:      subClient,
		account:        opts.StripeAccount,
		tenantSalt:     opts.TenantSalt,
		ensemble:       opts.Ensemble,
		exporter:       opts.Exporter,
		retryCfg:       opts.RetryConfig,
		logger:         opts.Logger,
	}, nil
}

// Process handles a single webhook job with retry logic.
// It fetches canonical objects, sanitizes, runs matchers, and exports edges.
//
// Security: Only processes supported event types. Never trusts webhook payload
// for data - always fetches canonical objects from Stripe API.
func (w *Worker) Process(ctx context.Context, job Job) error {
	// Check if event type is supported
	if !SupportedEventTypes[job.Type] {
		w.logger.Debug("ignoring unsupported event type",
			"event_id", job.EventID,
			"type", job.Type)
		MetricsWebhookProcessedTotal.WithLabelValues("ignored").Inc()
		return nil
	}

	// Process with retry
	var lastErr error
	for attempt := 0; attempt < w.retryCfg.MaxAttempts; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := w.calculateBackoff(attempt)
			w.logger.Info("retrying webhook job after backoff",
				"event_id", job.EventID,
				"attempt", attempt+1,
				"backoff", backoff)

			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return fmt.Errorf("context canceled during retry backoff: %w", ctx.Err())
			}
		}

		err := w.processOnce(ctx, job)
		if err == nil {
			MetricsWebhookProcessedTotal.WithLabelValues("success").Inc()
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !isRetryableError(err) {
			w.logger.Error("non-retryable error processing webhook job",
				"event_id", job.EventID,
				"error", err)
			MetricsWebhookProcessedTotal.WithLabelValues("failed").Inc()
			return err
		}

		w.logger.Warn("retryable error processing webhook job",
			"event_id", job.EventID,
			"attempt", attempt+1,
			"error", err)
	}

	MetricsWebhookProcessedTotal.WithLabelValues("failed").Inc()
	return fmt.Errorf("webhook job failed after %d attempts: %w", w.retryCfg.MaxAttempts, lastErr)
}

// processOnce processes a job once without retry.
func (w *Worker) processOnce(ctx context.Context, job Job) error {
	// Determine object type and fetch canonical object(s)
	if strings.HasPrefix(job.Type, "invoice.") {
		return w.processInvoiceEvent(ctx, job)
	} else if strings.HasPrefix(job.Type, "customer.subscription.") {
		return w.processSubscriptionEvent(ctx, job)
	} else if strings.HasPrefix(job.Type, "charge.") {
		// Optional: charge events (MVP can skip)
		w.logger.Debug("charge event processing not yet implemented", "type", job.Type)
		return nil
	}

	return fmt.Errorf("unsupported event type: %s", job.Type)
}

// processInvoiceEvent fetches an invoice and related objects, then runs the matcher pipeline.
func (w *Worker) processInvoiceEvent(ctx context.Context, job Job) error {
	// Fetch invoice from Stripe using the backend client
	params := &stripe.InvoiceParams{}
	params.AddExpand("subscription")
	params.AddExpand("customer")
	if w.account != "" {
		params.SetStripeAccount(w.account)
	}

	inv, err := w.invoiceClient.Get(job.ObjectID, params)
	if err != nil {
		return fmt.Errorf("failed to fetch invoice %s: %w", job.ObjectID, err)
	}

	// Sanitize invoice
	sanitizedInv, err := stripec.SanitizeInvoice(w.tenantSalt, inv)
	if err != nil {
		return fmt.Errorf("failed to sanitize invoice: %w", err)
	}

	// Convert to lite format for matcher
	invLite := stripec.ToInvoiceLite(sanitizedInv)

	// For MVP, we skip fetching related subscriptions via webhook
	// The matcher pipeline can still work with just invoice data
	// TODO: In future, extract subscription ID from invoice and fetch if needed

	// Run matcher pipeline
	inputs := pipeline.Inputs{
		Usages: []matchers.UsageEvent{}, // No usage events for webhook (MVP)
		Subs:   []matchers.SubscriptionLite{}, // No subs for MVP
		Invs:   []matchers.InvoiceLite{invLite},
	}

	result, err := pipeline.Run(ctx, w.ensemble, w.exporter, inputs)
	if err != nil {
		return fmt.Errorf("pipeline failed for invoice event: %w", err)
	}

	w.logger.Info("webhook job processed successfully",
		"event_id", job.EventID,
		"type", job.Type,
		"edges_exported", result.Stats.ExportedEdges)

	return nil
}

// processSubscriptionEvent fetches a subscription and runs the matcher pipeline.
func (w *Worker) processSubscriptionEvent(ctx context.Context, job Job) error {
	// Fetch subscription from Stripe
	sub, err := w.fetchSubscription(ctx, job.ObjectID)
	if err != nil {
		return fmt.Errorf("failed to fetch subscription %s: %w", job.ObjectID, err)
	}

	// Sanitize subscription
	sanitizedSub, err := stripec.SanitizeSubscription(w.tenantSalt, sub)
	if err != nil {
		return fmt.Errorf("failed to sanitize subscription: %w", err)
	}

	// Convert to lite format
	subLite := stripec.ToSubscriptionLite(sanitizedSub)

	// Run matcher pipeline
	inputs := pipeline.Inputs{
		Usages: []matchers.UsageEvent{}, // No usage events for webhook (MVP)
		Subs:   []matchers.SubscriptionLite{subLite},
		Invs:   []matchers.InvoiceLite{}, // No invoices for subscription event
	}

	result, err := pipeline.Run(ctx, w.ensemble, w.exporter, inputs)
	if err != nil {
		return fmt.Errorf("pipeline failed for subscription event: %w", err)
	}

	w.logger.Info("webhook job processed successfully",
		"event_id", job.EventID,
		"type", job.Type,
		"edges_exported", result.Stats.ExportedEdges)

	return nil
}

// fetchSubscription retrieves a subscription with expanded items.
func (w *Worker) fetchSubscription(ctx context.Context, subID string) (*stripe.Subscription, error) {
	params := &stripe.SubscriptionParams{}
	params.AddExpand("items.data.price.product")
	if w.account != "" {
		params.SetStripeAccount(w.account)
	}

	return w.subClient.Get(subID, params)
}

// calculateBackoff computes exponential backoff with jitter.
func (w *Worker) calculateBackoff(attempt int) time.Duration {
	// Base * 2^attempt
	backoff := float64(w.retryCfg.BaseBackoff) * math.Pow(2, float64(attempt))

	// Cap at max backoff
	if backoff > float64(w.retryCfg.MaxBackoff) {
		backoff = float64(w.retryCfg.MaxBackoff)
	}

	return time.Duration(backoff)
}

// isRetryableError determines if an error should trigger a retry.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for Stripe-specific errors
	if stripeErr, ok := err.(*stripe.Error); ok {
		// Retry on rate limit and server errors
		switch stripeErr.Code {
		case stripe.ErrorCodeRateLimit:
			return true
		}

		// Retry on 5xx status codes
		if stripeErr.HTTPStatusCode >= 500 {
			return true
		}

		// Don't retry on 4xx errors (except rate limit)
		return false
	}

	// Retry on context deadline exceeded (may succeed with more time)
	if err == context.DeadlineExceeded {
		return true
	}

	// Default: don't retry unknown errors
	return false
}
