package exporter

import "sync"

// queue is a thread-safe, bounded queue for buffering export payloads.
// When the queue is full, the oldest item is dropped to make room for new items.
// This prevents the exporter from blocking producers or consuming unbounded memory.
type queue struct {
	ch     chan []byte
	mu     sync.Mutex
	closed bool
}

// newQueue creates a new queue with the specified capacity.
func newQueue(capacity int) *queue {
	return &queue{
		ch: make(chan []byte, capacity),
	}
}

// enqueue adds an item to the queue.
// If the queue is full, the oldest item is dropped to make room.
// This is a non-blocking operation.
func (q *queue) enqueue(data []byte) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		// Queue is closed, drop the item
		return
	}

	// Try non-blocking send
	select {
	case q.ch <- data:
		// Successfully enqueued
		return
	default:
		// Queue is full - drop oldest item and add new one
		select {
		case <-q.ch:
			// Dropped oldest item
		default:
			// This should not happen, but handle it gracefully
		}

		// Now add the new item
		select {
		case q.ch <- data:
			// Successfully enqueued after making room
		default:
			// Still couldn't enqueue - drop the new item
			// This can happen if queue was closed concurrently
		}
	}
}

// close closes the queue, preventing new enqueues.
// The channel is closed to signal the consumer (worker) to drain remaining items.
func (q *queue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.closed {
		q.closed = true
		close(q.ch)
	}
}
