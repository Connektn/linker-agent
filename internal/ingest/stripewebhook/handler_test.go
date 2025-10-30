package stripewebhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"linker-agent/internal/config"
)

// mockWorker is a no-op worker for testing the handler.
type mockWorker struct {
	processFunc func(ctx context.Context, job Job) error
}

func (m *mockWorker) Process(ctx context.Context, job Job) error {
	if m.processFunc != nil {
		return m.processFunc(ctx, job)
	}
	return nil
}

// generateTestSignature creates a valid Stripe signature for testing.
func generateTestSignature(payload []byte, secret string, timestamp int64) string {
	signedPayload := fmt.Sprintf("%d.%s", timestamp, payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedPayload))
	signature := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("t=%d,v1=%s", timestamp, signature)
}

func TestHandler_VerifySignature(t *testing.T) {
	secret := "whsec_test123"
	cfg := config.StripeWebhook{
		Enabled:       true,
		Path:          "/webhooks/stripe",
		SigningSecret: secret,
		MaxSkew:       300 * time.Second,
		Retry: config.StripeWebhookRetry{
			MaxAttempts: 3,
			BaseBackoff: 2 * time.Second,
			MaxBackoff:  30 * time.Second,
		},
	}

	worker := &mockWorker{}
	handler, err := NewHandler(HandlerOptions{
		Config:     cfg,
		Worker:     worker,
		Logger:     slog.Default(),
		QueueDepth: 10,
	})
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	ctx := context.Background()
	handler.Start(ctx)
	defer handler.Shutdown(context.Background())

	t.Run("valid signature", func(t *testing.T) {
		payload := []byte(`{"id":"evt_test","type":"invoice.created","created":` + fmt.Sprintf("%d", time.Now().Unix()) + `,"data":{"object":{"id":"in_test"}}}`)
		signature := generateTestSignature(payload, secret, time.Now().Unix())

		req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(string(payload)))
		req.Header.Set("Stripe-Signature", signature)

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})

	t.Run("invalid signature", func(t *testing.T) {
		payload := []byte(`{"id":"evt_test","type":"invoice.created","created":` + fmt.Sprintf("%d", time.Now().Unix()) + `,"data":{"object":{"id":"in_test"}}}`)
		signature := generateTestSignature(payload, "wrong_secret", time.Now().Unix())

		req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(string(payload)))
		req.Header.Set("Stripe-Signature", signature)

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", w.Code)
		}
	})

	t.Run("missing signature", func(t *testing.T) {
		payload := []byte(`{"id":"evt_test","type":"invoice.created","created":` + fmt.Sprintf("%d", time.Now().Unix()) + `,"data":{"object":{"id":"in_test"}}}`)

		req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(string(payload)))
		// No Stripe-Signature header

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", w.Code)
		}
	})

	t.Run("old timestamp beyond maxSkew", func(t *testing.T) {
		oldTimestamp := time.Now().Add(-400 * time.Second).Unix() // Beyond 300s maxSkew
		payload := []byte(`{"id":"evt_test","type":"invoice.created","created":` + fmt.Sprintf("%d", oldTimestamp) + `,"data":{"object":{"id":"in_test"}}}`)
		signature := generateTestSignature(payload, secret, oldTimestamp)

		req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(string(payload)))
		req.Header.Set("Stripe-Signature", signature)

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", w.Code)
		}
	})
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	cfg := config.StripeWebhook{
		Enabled:       true,
		Path:          "/webhooks/stripe",
		SigningSecret: "whsec_test123",
		MaxSkew:       300 * time.Second,
		Retry: config.StripeWebhookRetry{
			MaxAttempts: 3,
			BaseBackoff: 2 * time.Second,
			MaxBackoff:  30 * time.Second,
		},
	}

	worker := &mockWorker{}
	handler, err := NewHandler(HandlerOptions{
		Config:     cfg,
		Worker:     worker,
		Logger:     slog.Default(),
		QueueDepth: 10,
	})
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	// Test GET request (should be rejected)
	req := httptest.NewRequest(http.MethodGet, "/webhooks/stripe", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestHandler_Idempotency(t *testing.T) {
	secret := "whsec_test123"
	cfg := config.StripeWebhook{
		Enabled:       true,
		Path:          "/webhooks/stripe",
		SigningSecret: secret,
		MaxSkew:       300 * time.Second,
		Retry: config.StripeWebhookRetry{
			MaxAttempts: 3,
			BaseBackoff: 2 * time.Second,
			MaxBackoff:  30 * time.Second,
		},
	}

	processCount := 0
	worker := &mockWorker{
		processFunc: func(ctx context.Context, job Job) error {
			processCount++
			return nil
		},
	}

	handler, err := NewHandler(HandlerOptions{
		Config:     cfg,
		Worker:     worker,
		Logger:     slog.Default(),
		QueueDepth: 10,
	})
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	ctx := context.Background()
	handler.Start(ctx)
	defer handler.Shutdown(context.Background())

	eventID := "evt_test_" + fmt.Sprintf("%d", time.Now().UnixNano())
	payload := []byte(fmt.Sprintf(`{"id":"%s","type":"invoice.created","created":%d,"data":{"object":{"id":"in_test"}}}`,
		eventID, time.Now().Unix()))
	signature := generateTestSignature(payload, secret, time.Now().Unix())

	// First request - should be processed
	req1 := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(string(payload)))
	req1.Header.Set("Stripe-Signature", signature)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("First request: Expected status 200, got %d", w1.Code)
	}

	// Wait a bit for processing
	time.Sleep(100 * time.Millisecond)

	initialCount := processCount

	// Second request with same event ID - should be ignored (idempotent)
	req2 := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(string(payload)))
	req2.Header.Set("Stripe-Signature", signature)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("Second request: Expected status 200, got %d", w2.Code)
	}

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Process count should not have increased
	if processCount != initialCount {
		t.Errorf("Expected process count to stay at %d, but got %d (duplicate was processed)", initialCount, processCount)
	}
}

func TestHandler_IPAllowlist(t *testing.T) {
	secret := "whsec_test123"
	cfg := config.StripeWebhook{
		Enabled:         true,
		Path:            "/webhooks/stripe",
		SigningSecret:   secret,
		MaxSkew:         300 * time.Second,
		AllowedIPRanges: []string{"192.168.1.0/24", "10.0.0.0/8"},
		Retry: config.StripeWebhookRetry{
			MaxAttempts: 3,
			BaseBackoff: 2 * time.Second,
			MaxBackoff:  30 * time.Second,
		},
	}

	worker := &mockWorker{}
	handler, err := NewHandler(HandlerOptions{
		Config:     cfg,
		Worker:     worker,
		Logger:     slog.Default(),
		QueueDepth: 10,
	})
	if err != nil {
		t.Fatalf("Failed to create handler: %v", err)
	}

	ctx := context.Background()
	handler.Start(ctx)
	defer handler.Shutdown(context.Background())

	t.Run("allowed IP", func(t *testing.T) {
		payload := []byte(`{"id":"evt_test","type":"invoice.created","created":` + fmt.Sprintf("%d", time.Now().Unix()) + `,"data":{"object":{"id":"in_test"}}}`)
		signature := generateTestSignature(payload, secret, time.Now().Unix())

		req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(string(payload)))
		req.Header.Set("Stripe-Signature", signature)
		req.RemoteAddr = "192.168.1.100:12345" // Within allowed range

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})

	t.Run("disallowed IP", func(t *testing.T) {
		payload := []byte(`{"id":"evt_test2","type":"invoice.created","created":` + fmt.Sprintf("%d", time.Now().Unix()) + `,"data":{"object":{"id":"in_test"}}}`)
		signature := generateTestSignature(payload, secret, time.Now().Unix())

		req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", strings.NewReader(string(payload)))
		req.Header.Set("Stripe-Signature", signature)
		req.RemoteAddr = "172.16.0.1:12345" // Not in allowed ranges

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", w.Code)
		}
	})
}
