package queue

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// MemoryQueue implements Queue interface using in-memory buffered channels (legacy)
// This is kept for backward compatibility and testing
type MemoryQueue struct {
	stream Stream
	ch     chan QueueItem
	mu     sync.Mutex
	closed bool

	// Offset tracking
	writeOffset atomic.Uint64
	readOffset  atomic.Uint64

	// Metrics
	droppedCount   atomic.Uint64
	enqueuedCount  atomic.Uint64
	dequeuedCount  atomic.Uint64
	committedCount atomic.Uint64

	// Drop policy
	dropPolicy DropPolicy
	maxEvents  int64
}

// NewMemoryQueue creates a new in-memory queue
func NewMemoryQueue(stream Stream, capacity int, dropPolicy DropPolicy) *MemoryQueue {
	if capacity <= 0 {
		capacity = 1000
	}

	return &MemoryQueue{
		stream:     stream,
		ch:         make(chan QueueItem, capacity),
		dropPolicy: dropPolicy,
		maxEvents:  int64(capacity),
	}
}

// Enqueue adds an item to the queue
func (q *MemoryQueue) Enqueue(ctx context.Context, data []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return ErrQueueClosed
	}

	offset := q.writeOffset.Add(1) - 1
	item := QueueItem{
		Offset: offset,
		Data:   data,
	}

	// Try non-blocking send
	select {
	case q.ch <- item:
		q.enqueuedCount.Add(1)
		return nil
	default:
		// Queue is full
		if q.dropPolicy == RejectNew {
			q.writeOffset.Add(^uint64(0)) // Decrement
			return ErrQueueFull
		}

		// DropOldest - remove oldest and add new
		select {
		case <-q.ch:
			q.droppedCount.Add(1)
			q.readOffset.Add(1)
		default:
		}

		// Try again
		select {
		case q.ch <- item:
			q.enqueuedCount.Add(1)
			return nil
		default:
			q.writeOffset.Add(^uint64(0)) // Decrement
			q.droppedCount.Add(1)
			return fmt.Errorf("queue: failed to enqueue after drop")
		}
	}
}

// DequeueBatch retrieves up to batchSize items from the queue
func (q *MemoryQueue) DequeueBatch(ctx context.Context, batchSize int) ([]QueueItem, error) {
	items := make([]QueueItem, 0, batchSize)

	for i := 0; i < batchSize; i++ {
		select {
		case item := <-q.ch:
			items = append(items, item)
		default:
			// No more items available
			return items, nil
		}
	}

	if len(items) > 0 {
		q.dequeuedCount.Add(uint64(len(items)))
	}

	return items, nil
}

// Commit marks items as processed (no-op for memory queue)
func (q *MemoryQueue) Commit(ctx context.Context, offset uint64) error {
	q.committedCount.Add(1)
	q.readOffset.Store(offset + 1)
	return nil
}

// MoveToDLQ is not supported for memory queues
func (q *MemoryQueue) MoveToDLQ(ctx context.Context, item QueueItem, reason string, err error) error {
	return fmt.Errorf("memory queue: DLQ not supported")
}

// Metrics returns current queue metrics
func (q *MemoryQueue) Metrics() Metrics {
	return Metrics{
		Depth:          len(q.ch),
		DLQSize:        0,
		DroppedCount:   q.droppedCount.Load(),
		EnqueuedCount:  q.enqueuedCount.Load(),
		DequeuedCount:  q.dequeuedCount.Load(),
		CommittedCount: q.committedCount.Load(),
		BytesUsed:      0, // Not tracked for memory queue
	}
}

// Close closes the queue
func (q *MemoryQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.closed {
		q.closed = true
		close(q.ch)
	}

	return nil
}
