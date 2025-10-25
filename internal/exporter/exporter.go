// Package exporter provides a reliable, privacy-preserving HTTP exporter for
// transmitting synthetic link data (no PII) to the Connektn Cloud ingestion API.
//
// The exporter batches payloads, implements retry logic with exponential backoff,
// and ensures graceful shutdown without data loss.
//
// Security: Payloads are never logged. Only summary metrics (count, status) are
// logged to prevent accidental PII leakage. All transmission occurs over HTTPS.
//
// Threat model: Assumes TLS is properly configured and tenant keys are managed
// securely. The exporter does not validate payload contents - it trusts that
// upstream components (matchers, sanitizers) have already removed PII.
package exporter

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// Exporter batches and transmits synthetic link data to Connektn Cloud.
// It runs a background worker that periodically flushes queued payloads via HTTPS
// and/or writes them to a local file, depending on the configured mode.
type Exporter struct {
	endpoint   string
	tenantKey  string
	client     *http.Client
	queue      *queue
	batchSize  int
	flushEvery time.Duration
	maxRetries int

	// Export sink configuration
	mode     string // "http" | "file" | "both"
	filePath string
	fileMu   sync.Mutex // protects concurrent file writes

	// Concurrency control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Options configures the Exporter behavior.
type Options struct {
	// Mode determines export sink behavior: "http", "file", or "both".
	// Default: "http"
	Mode string

	// Endpoint is the Connektn Cloud ingestion API URL.
	// Required if Mode is "http" or "both".
	// Example: "https://api.connektn.dev/ingest"
	Endpoint string

	// FilePath is the local file path for file sink output.
	// Required if Mode is "file" or "both".
	// Example: "reports/exporter_output.jsonl"
	FilePath string

	// TenantKey is the authentication key for this tenant.
	// This is not secret but must be transmitted over HTTPS.
	TenantKey string

	// BatchSize is the maximum number of events to batch before flushing.
	// Default: 50
	BatchSize int

	// FlushEvery is the maximum time to wait before flushing a partial batch.
	// Default: 5s
	FlushEvery time.Duration

	// MaxRetries is the maximum number of retry attempts for failed requests.
	// Default: 3
	MaxRetries int

	// QueueCapacity is the buffered channel capacity for queued events.
	// Default: 5 × BatchSize
	QueueCapacity int
}

// New creates a new Exporter with the given options.
// Returns an error if required options are missing or invalid.
func New(opts Options) (*Exporter, error) {
	// Default mode to "http" if not specified
	if opts.Mode == "" {
		opts.Mode = "http"
	}

	// Validate mode
	if opts.Mode != "http" && opts.Mode != "file" && opts.Mode != "both" {
		return nil, fmt.Errorf("exporter: mode must be 'http', 'file', or 'both'")
	}

	// Validate required fields based on mode
	if opts.Mode == "http" || opts.Mode == "both" {
		if opts.Endpoint == "" {
			return nil, fmt.Errorf("exporter: endpoint is required when mode is 'http' or 'both'")
		}
		if opts.TenantKey == "" {
			return nil, fmt.Errorf("exporter: tenant key is required when mode is 'http' or 'both'")
		}
	}

	if opts.Mode == "file" || opts.Mode == "both" {
		if opts.FilePath == "" {
			return nil, fmt.Errorf("exporter: filePath is required when mode is 'file' or 'both'")
		}
	}

	// Apply defaults
	if opts.BatchSize <= 0 {
		opts.BatchSize = 50
	}
	if opts.FlushEvery <= 0 {
		opts.FlushEvery = 5 * time.Second
	}
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = 3
	}
	if opts.QueueCapacity <= 0 {
		opts.QueueCapacity = opts.BatchSize * 5
	}

	// Create HTTP client with sensible timeouts
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			IdleConnTimeout: 30 * time.Second,
			MaxIdleConns:    10,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Exporter{
		endpoint:   opts.Endpoint,
		tenantKey:  opts.TenantKey,
		client:     client,
		queue:      newQueue(opts.QueueCapacity),
		batchSize:  opts.BatchSize,
		flushEvery: opts.FlushEvery,
		maxRetries: opts.MaxRetries,
		mode:       opts.Mode,
		filePath:   opts.FilePath,
		ctx:        ctx,
		cancel:     cancel,
	}, nil
}

// Enqueue adds a payload to the export queue.
// The payload is serialized to JSON and buffered for batch transmission.
//
// If the queue is full, the oldest entry is dropped to make room.
// This is a non-blocking operation.
//
// Parameters:
//   - ctx: context for cancellation (currently unused but reserved for future timeout control)
//   - payload: any serializable struct (typically from internal/models)
//
// Returns an error if JSON serialization fails.
func (e *Exporter) Enqueue(ctx context.Context, payload any) error {
	// Serialize payload to JSON
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("exporter enqueue: json marshal failed: %w", err)
	}

	// Add to queue (non-blocking, drops oldest if full)
	e.queue.enqueue(data)

	// TODO: Add metric counter for dropped events when queue is full
	return nil
}

// Start begins the background worker that batches and flushes events.
// This method returns immediately; the worker runs in a goroutine.
//
// The worker will continue running until Shutdown is called.
func (e *Exporter) Start(ctx context.Context) {
	e.wg.Add(1)
	go e.worker()
}

// Shutdown gracefully stops the exporter.
// It stops accepting new events, flushes remaining queued events,
// and waits for in-flight requests to complete.
//
// The context timeout controls how long to wait for graceful shutdown.
// If the timeout expires, shutdown may be incomplete.
func (e *Exporter) Shutdown(ctx context.Context) error {
	// Close queue to prevent new enqueues and signal worker
	e.queue.close()

	// Cancel context to wake up worker if it's waiting on ticker
	e.cancel()

	// Wait for worker to finish (with timeout from context)
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("exporter shutdown: %w", ctx.Err())
	}
}

// worker is the background goroutine that batches and flushes events.
func (e *Exporter) worker() {
	defer e.wg.Done()

	ticker := time.NewTicker(e.flushEvery)
	defer ticker.Stop()

	var batch [][]byte

	for {
		select {
		case data, ok := <-e.queue.ch:
			if !ok {
				// Queue closed - flush remaining batch and exit
				if len(batch) > 0 {
					e.flush(batch)
				}
				return
			}

			// Accumulate event
			batch = append(batch, data)

			// Flush if batch size reached
			if len(batch) >= e.batchSize {
				e.flush(batch)
				batch = nil
			}

		case <-ticker.C:
			// Periodic flush
			if len(batch) > 0 {
				e.flush(batch)
				batch = nil
			}

		case <-e.ctx.Done():
			// Context canceled (shouldn't happen in normal shutdown)
			// Flush remaining batch
			// fmt.Printf("DEBUG: Context canceled, batch size=%d\n", len(batch))
			if len(batch) > 0 {
				e.flush(batch)
			}
			return
		}
	}
}

// flush transmits a batch of events to configured sinks (HTTP and/or file).
// It implements retry logic with exponential backoff for HTTP errors.
// Errors from one sink do not block the other.
func (e *Exporter) flush(batch [][]byte) {
	if len(batch) == 0 {
		return
	}

	// Build JSON array payload
	// We need to construct a valid JSON array from the individual JSON objects
	var jsonObjects []json.RawMessage
	for _, data := range batch {
		jsonObjects = append(jsonObjects, json.RawMessage(data))
	}

	payload, err := json.Marshal(jsonObjects)
	if err != nil {
		// This should never happen since we already validated JSON during enqueue
		// But if it does, log and discard the batch
		// TODO: Add error metric
		return
	}

	// Write to file sink if enabled
	if e.mode == "file" || e.mode == "both" {
		if err := e.writeToFile(payload); err != nil {
			// Log warning but continue - don't let file errors block HTTP
			// TODO: Add file write error metric
		}
	}

	// Send to HTTP sink if enabled
	if e.mode == "http" || e.mode == "both" {
		// Attempt delivery with retry
		for attempt := 0; attempt <= e.maxRetries; attempt++ {
			if attempt > 0 {
				// Exponential backoff: 2s, 4s, 8s, ...
				backoff := time.Duration(1<<uint(attempt-1)) * 2 * time.Second
				time.Sleep(backoff)
			}

			if err := e.send(payload); err != nil {
				// Check if we should retry
				if attempt < e.maxRetries && isRetryable(err) {
					// TODO: Add retry metric
					continue
				}
				// Max retries exceeded or non-retryable error - discard batch
				// TODO: Add failed batch metric
				return
			}

			// Success
			// TODO: Add success metric (count: len(batch))
			return
		}
	}
}

// writeToFile appends a JSON payload to the configured file sink.
// Each payload is written as a single line (JSONL format).
// File writes are protected by a mutex to prevent concurrent write corruption.
//
// Security: This function never logs payload contents to prevent PII leakage.
// Only error conditions are logged with generic messages.
func (e *Exporter) writeToFile(data []byte) error {
	e.fileMu.Lock()
	defer e.fileMu.Unlock()

	// Open file in append mode, create if it doesn't exist
	f, err := os.OpenFile(e.filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("exporter: failed to open file sink: %w", err)
	}
	defer f.Close()

	// Write payload followed by newline (JSONL format)
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("exporter: failed to write to file sink: %w", err)
	}
	if _, err := f.Write([]byte("\n")); err != nil {
		return fmt.Errorf("exporter: failed to write newline to file sink: %w", err)
	}

	return nil
}

// send performs a single HTTP POST to the ingestion endpoint.
func (e *Exporter) send(payload []byte) error {
	// Create request with a fresh context (not e.ctx which may be canceled during shutdown)
	// Use a timeout context to ensure requests don't hang indefinitely
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(payload))
	if err != nil {
		return &exportError{statusCode: 0, err: err, retryable: false}
	}

	// Set headers
	req.Header.Set("Authorization", "Bearer "+e.tenantKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", generateUUID())

	// Send request
	resp, err := e.client.Do(req)
	if err != nil {
		// Network error - retryable
		return &exportError{statusCode: 0, err: err, retryable: true}
	}
	defer resp.Body.Close()

	// Drain response body to enable connection reuse
	_, _ = io.Copy(io.Discard, resp.Body)

	// Check status code
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil // Success
	}

	// Determine if error is retryable
	retryable := resp.StatusCode >= 500

	return &exportError{
		statusCode: resp.StatusCode,
		err:        fmt.Errorf("http %d", resp.StatusCode),
		retryable:  retryable,
	}
}

// exportError wraps HTTP errors with retry metadata.
type exportError struct {
	statusCode int
	err        error
	retryable  bool
}

func (e *exportError) Error() string {
	return fmt.Sprintf("export failed: %v", e.err)
}

// isRetryable determines if an error should trigger a retry.
func isRetryable(err error) bool {
	if ee, ok := err.(*exportError); ok {
		return ee.retryable
	}
	return false
}

// generateUUID generates a UUID v4 using crypto/rand.
// This is used for idempotency keys to ensure requests can be safely retried.
func generateUUID() string {
	// Generate 16 random bytes
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		// This should never happen, but fall back to a deterministic value
		// based on current time if crypto/rand fails
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}

	// Set version (4) and variant bits according to RFC 4122
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant 10

	// Format as UUID string: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
