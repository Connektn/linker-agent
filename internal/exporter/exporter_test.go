package exporter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestNew verifies constructor validation and defaults.
func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		wantErr bool
	}{
		{
			name: "valid with all options",
			opts: Options{
				Endpoint:   "https://api.test.dev/ingest",
				TenantKey:  "tenant_123",
				BatchSize:  100,
				FlushEvery: 10 * time.Second,
				MaxRetries: 5,
			},
			wantErr: false,
		},
		{
			name: "valid with defaults",
			opts: Options{
				Endpoint:  "https://api.test.dev/ingest",
				TenantKey: "tenant_123",
			},
			wantErr: false,
		},
		{
			name: "missing endpoint",
			opts: Options{
				TenantKey: "tenant_123",
			},
			wantErr: true,
		},
		{
			name: "missing tenant key",
			opts: Options{
				Endpoint: "https://api.test.dev/ingest",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exp, err := New(tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if exp == nil {
					t.Error("New() returned nil exporter")
				}
				// Verify defaults were applied
				if exp.batchSize <= 0 {
					t.Error("batchSize should have default value")
				}
				if exp.flushEvery <= 0 {
					t.Error("flushEvery should have default value")
				}
				if exp.maxRetries <= 0 {
					t.Error("maxRetries should have default value")
				}
			}
		})
	}
}

// TestEnqueue verifies that payloads are correctly serialized and queued.
func TestEnqueue(t *testing.T) {
	exp, err := New(Options{
		Endpoint:      "https://api.test.dev/ingest",
		TenantKey:     "tenant_123",
		QueueCapacity: 2, // Small capacity for testing overflow
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer exp.Shutdown(context.Background())

	ctx := context.Background()

	// Test successful enqueue
	payload := map[string]string{"id": "test_1", "type": "subscription"}
	if err := exp.Enqueue(ctx, payload); err != nil {
		t.Errorf("Enqueue() error = %v", err)
	}

	// Verify item is in queue
	select {
	case data := <-exp.queue.ch:
		var decoded map[string]string
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Failed to unmarshal queued data: %v", err)
		}
		if decoded["id"] != "test_1" {
			t.Errorf("Expected id 'test_1', got '%s'", decoded["id"])
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Item not found in queue")
	}

	// Test enqueue with invalid JSON (should fail)
	invalidPayload := make(chan int) // channels can't be marshaled
	if err := exp.Enqueue(ctx, invalidPayload); err == nil {
		t.Error("Enqueue() should fail with unmarshallable payload")
	}
}

// TestEnqueueOverflow verifies that the queue drops oldest items when full.
func TestEnqueueOverflow(t *testing.T) {
	exp, err := New(Options{
		Endpoint:      "https://api.test.dev/ingest",
		TenantKey:     "tenant_123",
		QueueCapacity: 2, // Small capacity to trigger overflow
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	defer exp.Shutdown(context.Background())

	ctx := context.Background()

	// Fill the queue
	_ = exp.Enqueue(ctx, map[string]string{"id": "1"})
	_ = exp.Enqueue(ctx, map[string]string{"id": "2"})

	// This should drop the oldest item (id: "1")
	_ = exp.Enqueue(ctx, map[string]string{"id": "3"})

	// Verify queue contains "2" and "3", not "1"
	time.Sleep(10 * time.Millisecond) // Give enqueue time to complete

	var found []string
	for i := 0; i < 2; i++ {
		select {
		case data := <-exp.queue.ch:
			var decoded map[string]string
			json.Unmarshal(data, &decoded)
			found = append(found, decoded["id"])
		case <-time.After(100 * time.Millisecond):
			break
		}
	}

	// We should have 2 items
	if len(found) != 2 {
		t.Errorf("Expected 2 items in queue, got %d", len(found))
	}

	// Check that "1" is not present (it was dropped)
	for _, id := range found {
		if id == "1" {
			t.Error("Oldest item '1' should have been dropped")
		}
	}
}

// TestFlushSuccess verifies successful batch transmission.
func TestFlushSuccess(t *testing.T) {
	var requestCount int32
	var receivedPayload []byte
	var mu sync.Mutex

	// Mock server that returns 200 OK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)

		// Verify headers
		if auth := r.Header.Get("Authorization"); auth != "Bearer tenant_123" {
			t.Errorf("Expected Authorization header 'Bearer tenant_123', got '%s'", auth)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Expected Content-Type 'application/json', got '%s'", ct)
		}
		if idem := r.Header.Get("Idempotency-Key"); idem == "" {
			t.Error("Expected Idempotency-Key header")
		}

		// Capture payload
		mu.Lock()
		receivedPayload, _ = io.ReadAll(r.Body)
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exp, err := New(Options{
		Endpoint:   server.URL,
		TenantKey:  "tenant_123",
		BatchSize:  2,
		FlushEvery: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	// Start worker
	exp.Start(context.Background())

	// Enqueue items
	ctx := context.Background()
	_ = exp.Enqueue(ctx, map[string]string{"id": "1"})
	_ = exp.Enqueue(ctx, map[string]string{"id": "2"})

	// Wait for flush (should happen when batch size reached)
	time.Sleep(200 * time.Millisecond)

	// Shutdown and verify
	exp.Shutdown(context.Background())

	if count := atomic.LoadInt32(&requestCount); count != 1 {
		t.Errorf("Expected 1 HTTP request, got %d", count)
	}

	// Verify payload is a valid JSON array
	mu.Lock()
	var payloadArray []map[string]string
	if err := json.Unmarshal(receivedPayload, &payloadArray); err != nil {
		t.Fatalf("Received payload is not valid JSON: %v", err)
	}
	mu.Unlock()

	if len(payloadArray) != 2 {
		t.Errorf("Expected 2 items in payload, got %d", len(payloadArray))
	}
}

// TestFlushRetry verifies retry logic on 5xx errors.
func TestFlushRetry(t *testing.T) {
	var requestCount int32

	// Mock server that returns 500 on first 2 attempts, then 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		if count <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	exp, err := New(Options{
		Endpoint:   server.URL,
		TenantKey:  "tenant_123",
		BatchSize:  1,
		FlushEvery: 50 * time.Millisecond,
		MaxRetries: 3,
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	exp.Start(context.Background())

	// Enqueue one item
	ctx := context.Background()
	_ = exp.Enqueue(ctx, map[string]string{"id": "1"})

	// Wait for retries to complete
	// Backoff: attempt 1 (0s), attempt 2 (+2s), attempt 3 (+4s) = 6s total
	// Add buffer for processing
	time.Sleep(7 * time.Second)

	exp.Shutdown(context.Background())

	// Should have made 3 attempts (1 initial + 2 retries)
	if count := atomic.LoadInt32(&requestCount); count != 3 {
		t.Errorf("Expected 3 HTTP requests (with retries), got %d", count)
	}
}

// TestFlushDiscard verifies that 4xx errors cause batch discard.
func TestFlushDiscard(t *testing.T) {
	var requestCount int32

	// Mock server that returns 400 Bad Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	exp, err := New(Options{
		Endpoint:   server.URL,
		TenantKey:  "tenant_123",
		BatchSize:  1,
		FlushEvery: 50 * time.Millisecond,
		MaxRetries: 3,
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	exp.Start(context.Background())

	// Enqueue one item
	ctx := context.Background()
	_ = exp.Enqueue(ctx, map[string]string{"id": "1"})

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	exp.Shutdown(context.Background())

	// Should have made only 1 attempt (no retries for 4xx)
	if count := atomic.LoadInt32(&requestCount); count != 1 {
		t.Errorf("Expected 1 HTTP request (no retries for 4xx), got %d", count)
	}
}

// TestShutdown verifies graceful shutdown with final flush.
func TestShutdown(t *testing.T) {
	var requestCount int32
	var mu sync.Mutex
	var receivedPayloads [][]byte
	requestReceived := make(chan struct{}, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		t.Logf("HTTP request #%d received", count)

		payload, _ := io.ReadAll(r.Body)
		mu.Lock()
		receivedPayloads = append(receivedPayloads, payload)
		mu.Unlock()

		w.WriteHeader(http.StatusOK)

		select {
		case requestReceived <- struct{}{}:
		default:
		}
	}))
	defer server.Close()

	exp, err := New(Options{
		Endpoint:   server.URL,
		TenantKey:  "tenant_123",
		BatchSize:  10,                     // Large batch size
		FlushEvery: 1 * time.Hour,          // Long flush interval
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	exp.Start(context.Background())

	// Enqueue items but don't wait for automatic flush
	ctx := context.Background()
	_ = exp.Enqueue(ctx, map[string]string{"id": "1"})
	_ = exp.Enqueue(ctx, map[string]string{"id": "2"})
	_ = exp.Enqueue(ctx, map[string]string{"id": "3"})

	// Wait for worker to consume all items from queue
	// The worker reads items from the channel into its internal batch
	for {
		if len(exp.queue.ch) == 0 {
			// All items consumed
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Give a bit more time to ensure worker has the items in its batch
	time.Sleep(50 * time.Millisecond)

	t.Logf("Shutting down exporter")

	// Shutdown should trigger final flush
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := exp.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}

	// Wait a bit for flush to complete
	select {
	case <-requestReceived:
		t.Logf("Request received during shutdown")
	case <-time.After(2 * time.Second):
		t.Log("Timeout waiting for request")
	}

	// Verify that a flush occurred
	if count := atomic.LoadInt32(&requestCount); count != 1 {
		t.Errorf("Expected 1 HTTP request during shutdown, got %d", count)
	}

	// Verify all items were flushed
	mu.Lock()
	if len(receivedPayloads) != 1 {
		t.Errorf("Expected 1 payload, got %d", len(receivedPayloads))
	}
	if len(receivedPayloads) > 0 {
		var items []map[string]string
		if err := json.Unmarshal(receivedPayloads[0], &items); err != nil {
			t.Fatalf("Failed to unmarshal payload: %v", err)
		}
		if len(items) != 3 {
			t.Errorf("Expected 3 items in final flush, got %d", len(items))
		}
	}
	mu.Unlock()
}

// TestPeriodicFlush verifies that the timer-based flush works.
func TestPeriodicFlush(t *testing.T) {
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exp, err := New(Options{
		Endpoint:   server.URL,
		TenantKey:  "tenant_123",
		BatchSize:  100,                    // Large batch size (won't be reached)
		FlushEvery: 100 * time.Millisecond, // Short interval for testing
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	exp.Start(context.Background())

	// Enqueue a single item
	ctx := context.Background()
	_ = exp.Enqueue(ctx, map[string]string{"id": "1"})

	// Wait for periodic flush
	time.Sleep(200 * time.Millisecond)

	exp.Shutdown(context.Background())

	// Should have flushed due to timer, not batch size
	if count := atomic.LoadInt32(&requestCount); count < 1 {
		t.Errorf("Expected at least 1 HTTP request from periodic flush, got %d", count)
	}
}

// TestExportErrorType verifies the exportError type behavior.
func TestExportErrorType(t *testing.T) {
	tests := []struct {
		name       string
		err        *exportError
		retryable  bool
		errMessage string
	}{
		{
			name: "retryable 500 error",
			err: &exportError{
				statusCode: 500,
				err:        http.ErrServerClosed,
				retryable:  true,
			},
			retryable:  true,
			errMessage: "export failed:",
		},
		{
			name: "non-retryable 400 error",
			err: &exportError{
				statusCode: 400,
				err:        http.ErrBodyNotAllowed,
				retryable:  false,
			},
			retryable:  false,
			errMessage: "export failed:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if isRetryable(tt.err) != tt.retryable {
				t.Errorf("isRetryable() = %v, want %v", isRetryable(tt.err), tt.retryable)
			}

			errMsg := tt.err.Error()
			if errMsg == "" {
				t.Error("Error() should return non-empty string")
			}
		})
	}
}

// TestQueueClose verifies that closing the queue prevents new enqueues.
func TestQueueClose(t *testing.T) {
	q := newQueue(10)

	// Enqueue before close
	q.enqueue([]byte("test1"))

	// Close queue
	q.close()

	// Enqueue after close (should be dropped)
	q.enqueue([]byte("test2"))

	// Drain queue - should only get "test1"
	count := 0
	for range q.ch {
		count++
	}

	if count != 1 {
		t.Errorf("Expected 1 item in queue after close, got %d", count)
	}
}
