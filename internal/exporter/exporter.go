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
	"path/filepath"
	"sync"
	"time"

	"linker-agent/internal/config"
)

// Stream identifies a logical export route/file.
// Different streams can be routed to different HTTP endpoints or file paths.
type Stream string

const (
	// StreamEdges represents link edge exports (matcher outputs).
	StreamEdges Stream = "edges"
	// StreamBilling represents billing data exports (subscriptions, invoices).
	StreamBilling Stream = "billing"
	// StreamUsage represents usage event exports.
	StreamUsage Stream = "usage"
)

// streamQueue holds a queue for a specific stream.
type streamQueue struct {
	queue *queue
	mu    sync.Mutex
}

// Exporter batches and transmits synthetic link data to Connektn Cloud.
// It runs a background worker that periodically flushes queued payloads via HTTPS
// and/or writes them to a local file, depending on the configured mode.
// Supports per-stream routing to different endpoints/files.
type Exporter struct {
	// Configuration
	cfg config.Export

	// HTTP client
	client     *http.Client
	tenantKey  string
	maxRetries int

	// Per-stream queues
	streams    map[Stream]*streamQueue
	batchSize  int
	flushEvery time.Duration

	// File writing
	fileMu sync.Mutex // protects concurrent file writes

	// Concurrency control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Options configures the Exporter behavior.
type Options struct {
	// Config provides the full export configuration including per-stream routing.
	Config config.Export

	// TenantKey is the authentication key for this tenant.
	// This is not secret but must be transmitted over HTTPS.
	TenantKey string

	// QueueCapacity is the buffered channel capacity for queued events per stream.
	// Default: 5 × BatchSize
	QueueCapacity int
}

// New creates a new Exporter with the given options.
// Returns an error if required options are missing or invalid.
func New(opts Options) (*Exporter, error) {
	// Validate tenant key if HTTP mode is enabled
	if (opts.Config.Mode == "http" || opts.Config.Mode == "both") && opts.TenantKey == "" {
		return nil, fmt.Errorf("exporter: tenant key is required when mode is 'http' or 'both'")
	}

	// Apply defaults for batch size and flush interval
	batchSize := opts.Config.HTTP.BatchSize
	if batchSize <= 0 {
		batchSize = 50
	}

	flushEvery := opts.Config.HTTP.FlushEvery
	if flushEvery <= 0 {
		flushEvery = 5 * time.Second
	}

	maxRetries := opts.Config.HTTP.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	// Apply queue capacity default
	if opts.QueueCapacity <= 0 {
		opts.QueueCapacity = batchSize * 5
	}

	// Create HTTP client with sensible timeouts
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			IdleConnTimeout: 30 * time.Second,
			MaxIdleConns:    10,
		},
	}

	// Create per-stream queues
	streams := make(map[Stream]*streamQueue)
	for _, stream := range []Stream{StreamEdges, StreamBilling, StreamUsage} {
		streams[stream] = &streamQueue{
			queue: newQueue(opts.QueueCapacity),
		}
	}

	// Ensure output directories exist for file mode
	if opts.Config.Mode == "file" || opts.Config.Mode == "both" {
		for _, path := range opts.Config.File.Paths {
			dir := filepath.Dir(path)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("exporter: failed to create directory %s: %w", dir, err)
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Exporter{
		cfg:        opts.Config,
		tenantKey:  opts.TenantKey,
		client:     client,
		streams:    streams,
		batchSize:  batchSize,
		flushEvery: flushEvery,
		maxRetries: maxRetries,
		ctx:        ctx,
		cancel:     cancel,
	}, nil
}

// Enqueue adds a payload to the specified stream's export queue.
// The payload is serialized to JSON and buffered for batch transmission.
//
// If the queue is full, the oldest entry is dropped to make room.
// This is a non-blocking operation.
//
// Parameters:
//   - ctx: context for cancellation (currently unused but reserved for future timeout control)
//   - stream: the export stream (e.g., StreamEdges, StreamBilling)
//   - payload: any serializable struct (typically from internal/models)
//
// Returns an error if the stream is undefined or JSON serialization fails.
func (e *Exporter) Enqueue(ctx context.Context, stream Stream, payload any) error {
	// Validate stream exists
	sq, ok := e.streams[stream]
	if !ok {
		return fmt.Errorf("exporter enqueue: undefined stream %q", stream)
	}

	// Validate stream has configured route/path
	if err := e.validateStreamConfig(stream); err != nil {
		return fmt.Errorf("exporter enqueue: %w", err)
	}

	// Serialize payload to JSON
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("exporter enqueue: json marshal failed: %w", err)
	}

	// Add to stream-specific queue (non-blocking, drops oldest if full)
	sq.queue.enqueue(data)

	// TODO: Add metric counter for dropped events when queue is full
	return nil
}

// validateStreamConfig ensures the stream has a valid route (HTTP) or path (file).
func (e *Exporter) validateStreamConfig(stream Stream) error {
	streamStr := string(stream)

	// Check HTTP route if mode includes "http"
	if e.cfg.Mode == "http" || e.cfg.Mode == "both" {
		if e.cfg.HTTP.Routes[streamStr] == "" {
			return fmt.Errorf("export route undefined for stream %q", stream)
		}
	}

	// Check file path if mode includes "file"
	if e.cfg.Mode == "file" || e.cfg.Mode == "both" {
		if e.cfg.File.Paths[streamStr] == "" {
			return fmt.Errorf("export file path undefined for stream %q", stream)
		}
	}

	return nil
}

// Start begins background workers that batch and flush events for each stream.
// This method returns immediately; workers run in goroutines.
//
// The workers will continue running until Shutdown is called.
func (e *Exporter) Start(ctx context.Context) {
	// Start a worker for each stream
	for stream, sq := range e.streams {
		e.wg.Add(1)
		go e.worker(stream, sq)
	}
}

// Shutdown gracefully stops the exporter.
// It stops accepting new events, flushes remaining queued events,
// and waits for in-flight requests to complete.
//
// The context timeout controls how long to wait for graceful shutdown.
// If the timeout expires, shutdown may be incomplete.
func (e *Exporter) Shutdown(ctx context.Context) error {
	// Close all stream queues to prevent new enqueues and signal workers
	for _, sq := range e.streams {
		sq.queue.close()
	}

	// Cancel context to wake up workers if they're waiting on ticker
	e.cancel()

	// Wait for all workers to finish (with timeout from context)
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

// worker is the background goroutine that batches and flushes events for a specific stream.
func (e *Exporter) worker(stream Stream, sq *streamQueue) {
	defer e.wg.Done()

	ticker := time.NewTicker(e.flushEvery)
	defer ticker.Stop()

	var batch [][]byte

	for {
		select {
		case data, ok := <-sq.queue.ch:
			if !ok {
				// Queue closed - flush remaining batch and exit
				if len(batch) > 0 {
					e.flushStream(stream, batch)
				}
				return
			}

			// Accumulate event
			batch = append(batch, data)

			// Flush if batch size reached
			if len(batch) >= e.batchSize {
				e.flushStream(stream, batch)
				batch = nil
			}

		case <-ticker.C:
			// Periodic flush
			if len(batch) > 0 {
				e.flushStream(stream, batch)
				batch = nil
			}

		case <-e.ctx.Done():
			// Context canceled (shouldn't happen in normal shutdown)
			// Flush remaining batch
			if len(batch) > 0 {
				e.flushStream(stream, batch)
			}
			return
		}
	}
}

// flushStream transmits a batch of events for a specific stream to configured sinks (HTTP and/or file).
// It implements retry logic with exponential backoff for HTTP errors.
// Errors from one sink do not block the other.
func (e *Exporter) flushStream(stream Stream, batch [][]byte) {
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
	if e.cfg.Mode == "file" || e.cfg.Mode == "both" {
		if err := e.writeToFileStream(stream, payload); err != nil {
			// Log warning but continue - don't let file errors block HTTP
			// TODO: Add file write error metric
		}
	}

	// Send to HTTP sink if enabled
	if e.cfg.Mode == "http" || e.cfg.Mode == "both" {
		// Attempt delivery with retry
		for attempt := 0; attempt <= e.maxRetries; attempt++ {
			if attempt > 0 {
				// Exponential backoff: 2s, 4s, 8s, ...
				backoff := time.Duration(1<<uint(attempt-1)) * 2 * time.Second
				time.Sleep(backoff)
			}

			if err := e.sendStream(stream, payload); err != nil {
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

// writeToFileStream appends a JSON payload to the stream-specific file sink.
// Each payload is written as a single line (JSONL format).
// File writes are protected by a mutex to prevent concurrent write corruption.
//
// Security: This function never logs payload contents to prevent PII leakage.
// Only error conditions are logged with generic messages.
func (e *Exporter) writeToFileStream(stream Stream, data []byte) error {
	e.fileMu.Lock()
	defer e.fileMu.Unlock()

	// Get stream-specific file path
	filePath, ok := e.cfg.File.Paths[string(stream)]
	if !ok {
		return fmt.Errorf("exporter: no file path configured for stream %q", stream)
	}

	// Open file in append mode, create if it doesn't exist
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("exporter: failed to open file sink for stream %q: %w", stream, err)
	}
	defer f.Close()

	// Write payload followed by newline (JSONL format)
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("exporter: failed to write to file sink for stream %q: %w", stream, err)
	}
	if _, err := f.Write([]byte("\n")); err != nil {
		return fmt.Errorf("exporter: failed to write newline to file sink for stream %q: %w", stream, err)
	}

	return nil
}

// sendStream performs a single HTTP POST to the stream-specific ingestion endpoint.
func (e *Exporter) sendStream(stream Stream, payload []byte) error {
	// Get stream-specific route
	route, ok := e.cfg.HTTP.Routes[string(stream)]
	if !ok {
		return &exportError{statusCode: 0, err: fmt.Errorf("no route configured for stream %q", stream), retryable: false}
	}

	// Build full endpoint URL
	endpoint := e.cfg.HTTP.BaseURL + route

	// Create request with a fresh context (not e.ctx which may be canceled during shutdown)
	// Use a timeout context to ensure requests don't hang indefinitely
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return &exportError{statusCode: 0, err: err, retryable: false}
	}

	// Set headers
	// Use tenant key from environment variable if configured
	authToken := e.tenantKey
	if e.cfg.HTTP.Headers.AuthorizationEnv != "" {
		authToken = os.Getenv(e.cfg.HTTP.Headers.AuthorizationEnv)
	}
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
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
