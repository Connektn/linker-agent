package stripewebhook

import (
	"context"
	"testing"
	"time"

	"linker-agent/internal/config"
)

func TestWorker_SupportedEventTypes(t *testing.T) {
	expectedTypes := []string{
		"invoice.created",
		"invoice.finalized",
		"invoice.payment_succeeded",
		"invoice.payment_failed",
		"customer.subscription.created",
		"customer.subscription.updated",
		"customer.subscription.deleted",
		"charge.succeeded",
		"charge.refunded",
	}

	for _, eventType := range expectedTypes {
		if !SupportedEventTypes[eventType] {
			t.Errorf("Expected event type %s to be supported", eventType)
		}
	}
}

func TestWorker_UnsupportedEventType(t *testing.T) {
	// Create a test job with unsupported event type
	job := Job{
		EventID:  "evt_test",
		Type:     "unknown.event.type",
		ObjectID: "obj_test",
		Created:  time.Now(),
	}

	// Validate that the event type is not in supported list
	if SupportedEventTypes[job.Type] {
		t.Errorf("Event type %s should not be supported", job.Type)
	}
}

func TestWorker_CalculateBackoff(t *testing.T) {
	retryCfg := config.StripeWebhookRetry{
		MaxAttempts: 5,
		BaseBackoff: 2 * time.Second,
		MaxBackoff:  30 * time.Second,
	}

	// Create a minimal worker struct just to test the backoff calculation
	w := &Worker{
		retryCfg: retryCfg,
	}

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 2 * time.Second},   // Base
		{1, 4 * time.Second},   // Base * 2^1
		{2, 8 * time.Second},   // Base * 2^2
		{3, 16 * time.Second},  // Base * 2^3
		{4, 30 * time.Second},  // Capped at MaxBackoff
		{5, 30 * time.Second},  // Still capped
	}

	for _, tt := range tests {
		actual := w.calculateBackoff(tt.attempt)
		if actual != tt.expected {
			t.Errorf("For attempt %d, expected backoff %v, got %v", tt.attempt, tt.expected, actual)
		}
	}
}

func TestWorker_IsRetryableError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name:      "nil error",
			err:       nil,
			retryable: false,
		},
		{
			name:      "context deadline exceeded",
			err:       context.DeadlineExceeded,
			retryable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := isRetryableError(tt.err)
			if actual != tt.retryable {
				t.Errorf("Expected isRetryableError(%v) = %v, got %v", tt.err, tt.retryable, actual)
			}
		})
	}
}

// TestWorker_Integration would require:
// 1. A mock Stripe API server or client
// 2. Mock exporter
// 3. Configured ensemble with matchers
// 4. Valid tenant salt
//
// This is deferred to integration tests with proper fixtures.
// For now, we validate the core logic (event type handling, backoff, retries).
