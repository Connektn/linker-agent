// Package queue provides durable, disk-backed queuing for the Connektn Linker Agent.
// It ensures zero data loss during network outages and agent restarts using BadgerDB
// for persistent storage with Write-Ahead Logging (WAL).
//
// Security: All queued data is already sanitized (synthetic IDs only) before enqueue.
// The queue stores no PII and never logs payload contents.
//
// Threat model: Assumes disk is not compromised. Queue files should be readable only
// by the agent process (chmod 700). No encryption at rest in this version.
package queue

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrQueueFull is returned when the queue has reached capacity and drop policy is reject_new
	ErrQueueFull = errors.New("queue: capacity limit reached")

	// ErrQueueClosed is returned when attempting to enqueue to a closed queue
	ErrQueueClosed = errors.New("queue: queue is closed")

	// ErrInvalidOffset is returned when trying to commit an invalid offset
	ErrInvalidOffset = errors.New("queue: invalid offset")
)

// DropPolicy defines how the queue behaves when full
type DropPolicy string

const (
	// DropOldest removes the oldest item to make room for new items
	// Suitable for metrics-like usage/feature queues where losing old data is acceptable
	DropOldest DropPolicy = "drop_oldest"

	// RejectNew returns ErrQueueFull when queue is full
	// Suitable for billing queues where we rely on upstream retry (e.g., Stripe)
	RejectNew DropPolicy = "reject_new"
)

// Stream identifies a logical queue stream
type Stream string

const (
	// StreamBilling for Stripe billing events (subscriptions, invoices)
	StreamBilling Stream = "billing"

	// StreamUsage for feature usage events
	StreamUsage Stream = "usage"

	// StreamEdges for identity link edges
	StreamEdges Stream = "edges"

	// StreamDLQ for dead letter queue (poison/invalid events)
	StreamDLQ Stream = "dlq"
)

// QueueItem represents a single item in the queue with its offset
type QueueItem struct {
	Offset uint64    // Monotonic offset in the queue
	Data   []byte    // Serialized event data (JSON)
	EnqueuedAt time.Time // When the item was enqueued
}

// Config holds configuration for a single queue stream
type Config struct {
	// Stream identifies this queue
	Stream Stream

	// WALPath is the directory for BadgerDB data
	WALPath string

	// MaxBytes is the maximum disk space this queue can use
	MaxBytes int64

	// MaxEvents is the maximum number of events this queue can hold
	MaxEvents int64

	// DropPolicy determines behavior when queue is full
	DropPolicy DropPolicy

	// SegmentSize is the approximate size of each BadgerDB value log segment (not used in BadgerDB mode)
	// Kept for future custom WAL implementation
	SegmentSize int64
}

// Metrics holds queue metrics for monitoring
type Metrics struct {
	Depth          int    // Current number of items in queue
	DLQSize        int    // Number of items in DLQ (0 for non-DLQ queues)
	DroppedCount   uint64 // Total items dropped when queue was full
	EnqueuedCount  uint64 // Total items successfully enqueued
	DequeuedCount  uint64 // Total items dequeued
	CommittedCount uint64 // Total items successfully committed (exported)
	BytesUsed      int64  // Approximate disk space used
}

// Queue is the interface for a durable queue
type Queue interface {
	// Enqueue adds an item to the queue
	// Returns ErrQueueFull if queue is full and policy is RejectNew
	// Returns ErrQueueClosed if queue is closed
	Enqueue(ctx context.Context, data []byte) error

	// DequeueBatch retrieves up to batchSize items from the queue
	// Items remain in queue until Commit is called
	DequeueBatch(ctx context.Context, batchSize int) ([]QueueItem, error)

	// Commit marks items up to and including the given offset as successfully processed
	// This removes them from the queue
	Commit(ctx context.Context, offset uint64) error

	// MoveToDLQ moves an item to the dead letter queue with a reason
	// Only valid for non-DLQ queues
	MoveToDLQ(ctx context.Context, item QueueItem, reason string, err error) error

	// Metrics returns current queue metrics
	Metrics() Metrics

	// Close gracefully closes the queue
	Close() error
}

// DLQEntry represents an entry in the dead letter queue
type DLQEntry struct {
	OriginalEvent  []byte    `json:"original_event"`
	Stream         Stream    `json:"stream"`
	Reason         string    `json:"reason"`          // "validation_error", "unknown_org", etc.
	ErrorMessage   string    `json:"error_message"`
	FirstAttemptAt time.Time `json:"first_attempt_at"`
	LastAttemptAt  time.Time `json:"last_attempt_at"`
	AttemptCount   int       `json:"attempt_count"`
	HTTPStatusCode int       `json:"http_status_code,omitempty"`
}

// formatKey creates a BadgerDB key for a queue item
// Format: "stream:offset" (e.g., "billing:00000000000000000123")
func formatKey(stream Stream, offset uint64) []byte {
	return []byte(fmt.Sprintf("%s:%020d", stream, offset))
}

// parseOffsetFromKey extracts the offset from a BadgerDB key
func parseOffsetFromKey(key []byte) (uint64, error) {
	// Key format: "stream:offset" (e.g., "billing:00000000000000000123")
	parts := bytes.SplitN(key, []byte(":"), 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid key format: %s", string(key))
	}

	var offset uint64
	_, err := fmt.Sscanf(string(parts[1]), "%d", &offset)
	return offset, err
}
