package stripewebhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"linker-agent/internal/config"
)

const (
	// MaxWebhookBodySize limits the size of incoming webhook payloads
	MaxWebhookBodySize = 1 * 1024 * 1024 // 1 MB
)

// Job represents a webhook event job to be processed.
// Only minimal metadata is stored for idempotency and worker processing.
type Job struct {
	EventID  string    // Stripe event ID (e.g., "evt_xxx")
	Type     string    // Event type (e.g., "invoice.payment_succeeded")
	ObjectID string    // Object ID from data.object.id (e.g., "in_xxx")
	Created  time.Time // Event creation timestamp
}

// stripeEvent is the minimal webhook event structure we parse.
// We only extract what's needed for idempotency and job creation.
type stripeEvent struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Created int64  `json:"created"`
	Data    struct {
		Object struct {
			ID string `json:"id"`
		} `json:"object"`
	} `json:"data"`
}

// JobProcessor interface allows for testability by accepting any processor implementation.
type JobProcessor interface {
	Process(ctx context.Context, job Job) error
}

// Handler manages webhook HTTP requests and dispatches jobs to workers.
type Handler struct {
	cfg    config.StripeWebhook
	worker JobProcessor
	logger *slog.Logger

	// Idempotency tracking
	seenMu sync.RWMutex
	seen   map[string]bool // event ID -> processed

	// Job queue
	jobs chan Job
}

// HandlerOptions configures the webhook handler.
type HandlerOptions struct {
	Config     config.StripeWebhook
	Worker     JobProcessor
	Logger     *slog.Logger
	QueueDepth int // Buffered channel capacity (default: 1000)
}

// NewHandler creates a new webhook handler.
func NewHandler(opts HandlerOptions) (*Handler, error) {
	if !opts.Config.Enabled {
		return nil, fmt.Errorf("webhook handler: webhook is not enabled")
	}

	if opts.Worker == nil {
		return nil, fmt.Errorf("webhook handler: worker is required")
	}

	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	if opts.QueueDepth <= 0 {
		opts.QueueDepth = 1000
	}

	return &Handler{
		cfg:    opts.Config,
		worker: opts.Worker,
		logger: opts.Logger,
		seen:   make(map[string]bool),
		jobs:   make(chan Job, opts.QueueDepth),
	}, nil
}

// Start begins processing jobs from the queue.
// This should be called before the HTTP handler is registered.
func (h *Handler) Start(ctx context.Context) {
	go h.processJobs(ctx)
}

// ServeHTTP implements http.Handler for the webhook endpoint.
// It verifies signatures, checks idempotency, and enqueues jobs.
//
// Security:
// - Verifies Stripe signature (HMAC-SHA256)
// - Optional IP allowlist
// - No request body logging (prevent PII leakage)
// - Returns 200 quickly to avoid Stripe retries
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only accept POST requests
	if r.Method != http.MethodPost {
		MetricsWebhookErrorsTotal.WithLabelValues("method_not_allowed").Inc()
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	MetricsWebhookReceivedTotal.Inc()

	// Verify IP allowlist if configured
	if len(h.cfg.AllowedIPRanges) > 0 {
		if err := VerifyIPAllowlist(r.RemoteAddr, h.cfg.AllowedIPRanges); err != nil {
			MetricsWebhookErrorsTotal.WithLabelValues("ip_not_allowed").Inc()
			h.logger.Warn("webhook request from disallowed IP",
				"remote_addr", r.RemoteAddr,
				"error", err)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	// Read body (limited size)
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxWebhookBodySize))
	if err != nil {
		MetricsWebhookErrorsTotal.WithLabelValues("body_read_error").Inc()
		h.logger.Error("failed to read webhook body", "error", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// Verify signature
	signature := r.Header.Get("Stripe-Signature")
	if err := VerifySignature(signature, body, h.cfg.SigningSecret, h.cfg.MaxSkew); err != nil {
		MetricsWebhookErrorsTotal.WithLabelValues("signature_invalid").Inc()
		h.logger.Warn("webhook signature verification failed", "error", err)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	MetricsWebhookVerifiedTotal.Inc()

	// Parse minimal event data
	var event stripeEvent
	if err := json.Unmarshal(body, &event); err != nil {
		MetricsWebhookErrorsTotal.WithLabelValues("json_parse_error").Inc()
		h.logger.Error("failed to parse webhook JSON", "error", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// Check idempotency
	if h.isDuplicate(event.ID) {
		MetricsWebhookDupDroppedTotal.Inc()
		h.logger.Debug("duplicate webhook event ignored", "event_id", event.ID)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Create job
	job := Job{
		EventID:  event.ID,
		Type:     event.Type,
		ObjectID: event.Data.Object.ID,
		Created:  time.Unix(event.Created, 0),
	}

	// Enqueue job (non-blocking)
	select {
	case h.jobs <- job:
		MetricsWebhookEnqueuedTotal.Inc()
		h.logger.Info("webhook event enqueued",
			"event_id", event.ID,
			"type", event.Type)
	default:
		// Queue full - this is a problem, but we still return 200
		// to avoid Stripe retrying immediately
		MetricsWebhookErrorsTotal.WithLabelValues("queue_full").Inc()
		h.logger.Error("webhook job queue full, dropping event",
			"event_id", event.ID,
			"type", event.Type)
	}

	// Return 200 immediately (job accepted)
	w.WriteHeader(http.StatusOK)
}

// isDuplicate checks if we've seen this event ID before.
// Thread-safe with read-write mutex.
func (h *Handler) isDuplicate(eventID string) bool {
	// Fast path: check with read lock
	h.seenMu.RLock()
	seen := h.seen[eventID]
	h.seenMu.RUnlock()

	if seen {
		return true
	}

	// Not seen yet - acquire write lock and mark as seen
	h.seenMu.Lock()
	defer h.seenMu.Unlock()

	// Double-check (another goroutine may have set it)
	if h.seen[eventID] {
		return true
	}

	h.seen[eventID] = true
	return false
}

// processJobs is the background worker that processes queued jobs.
// It runs until the context is canceled.
func (h *Handler) processJobs(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			h.logger.Info("webhook job processor shutting down")
			return

		case job := <-h.jobs:
			// Process job via worker
			if err := h.worker.Process(ctx, job); err != nil {
				h.logger.Error("failed to process webhook job",
					"event_id", job.EventID,
					"type", job.Type,
					"error", err)
			}
		}
	}
}

// Shutdown gracefully stops the handler.
// It stops accepting new jobs and waits for the worker to finish.
func (h *Handler) Shutdown(ctx context.Context) error {
	close(h.jobs)

	// Drain remaining jobs with timeout
	done := make(chan struct{})
	go func() {
		for job := range h.jobs {
			if err := h.worker.Process(ctx, job); err != nil {
				h.logger.Error("failed to process job during shutdown",
					"event_id", job.EventID,
					"error", err)
			}
		}
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("webhook handler shutdown timeout")
	}
}
