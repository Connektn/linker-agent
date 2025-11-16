package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// BadgerQueue implements the Queue interface using BadgerDB for persistent storage
type BadgerQueue struct {
	cfg    Config
	db     *badger.DB
	dlq    *BadgerQueue // Reference to DLQ (nil for DLQ itself)
	logger *slog.Logger

	// Offset tracking
	writeOffset atomic.Uint64 // Next offset to write
	readOffset  atomic.Uint64 // Next offset to read
	mu          sync.RWMutex  // Protects capacity checks and metrics updates

	// Metrics
	droppedCount   atomic.Uint64
	enqueuedCount  atomic.Uint64
	dequeuedCount  atomic.Uint64
	committedCount atomic.Uint64

	// State
	closed atomic.Bool
}

// NewBadgerQueue creates a new BadgerDB-backed durable queue
func NewBadgerQueue(cfg Config, dlq *BadgerQueue, logger *slog.Logger) (*BadgerQueue, error) {
	if cfg.WALPath == "" {
		return nil, fmt.Errorf("queue: WAL path is required")
	}

	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = 1 * 1024 * 1024 * 1024 // Default: 1GB
	}

	if cfg.MaxEvents <= 0 {
		cfg.MaxEvents = 100000 // Default: 100k events
	}

	if cfg.DropPolicy == "" {
		cfg.DropPolicy = DropOldest // Default policy
	}

	if logger == nil {
		logger = slog.Default()
	}

	// Open BadgerDB
	opts := badger.DefaultOptions(cfg.WALPath)
	opts.Logger = nil // Disable BadgerDB's default logger (noisy)

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("queue: failed to open badger db: %w", err)
	}

	q := &BadgerQueue{
		cfg:    cfg,
		db:     db,
		dlq:    dlq,
		logger: logger,
	}

	// Recover queue state from disk
	if err := q.recover(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("queue: recovery failed: %w", err)
	}

	// Start background garbage collection
	go q.runGC()

	logger.Info("durable queue initialized",
		"stream", cfg.Stream,
		"read_offset", q.readOffset.Load(),
		"write_offset", q.writeOffset.Load(),
		"depth", q.depth())

	return q, nil
}

// Enqueue adds an item to the queue
func (q *BadgerQueue) Enqueue(ctx context.Context, data []byte) error {
	if q.closed.Load() {
		return ErrQueueClosed
	}

	// Check capacity limits
	if err := q.checkCapacity(); err != nil {
		if q.cfg.DropPolicy == RejectNew {
			return err
		}
		// DropOldest policy - make room by removing oldest item
		if err := q.dropOldest(); err != nil {
			return fmt.Errorf("queue: failed to drop oldest: %w", err)
		}
	}

	// Write to BadgerDB
	offset := q.writeOffset.Add(1) - 1
	key := formatKey(q.cfg.Stream, offset)

	// Create queue item with timestamp
	item := struct {
		Data       []byte    `json:"data"`
		EnqueuedAt time.Time `json:"enqueued_at"`
	}{
		Data:       data,
		EnqueuedAt: time.Now().UTC(),
	}

	itemBytes, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("queue: failed to marshal item: %w", err)
	}

	err = q.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, itemBytes)
	})

	if err != nil {
		q.writeOffset.Add(^uint64(0)) // Decrement on failure
		return fmt.Errorf("queue: badger write failed: %w", err)
	}

	q.enqueuedCount.Add(1)

	return nil
}

// DequeueBatch retrieves up to batchSize items from the queue
func (q *BadgerQueue) DequeueBatch(ctx context.Context, batchSize int) ([]QueueItem, error) {
	if batchSize <= 0 {
		return nil, nil
	}

	items := make([]QueueItem, 0, batchSize)

	err := q.db.View(func(txn *badger.Txn) error {
		readOffset := q.readOffset.Load()
		writeOffset := q.writeOffset.Load()

		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		opts.PrefetchSize = batchSize

		it := txn.NewIterator(opts)
		defer it.Close()

		count := 0
		for offset := readOffset; offset < writeOffset && count < batchSize; offset++ {
			key := formatKey(q.cfg.Stream, offset)
			it.Seek(key)

			if !it.Valid() || string(it.Item().Key()) != string(key) {
				// Key doesn't exist (might have been deleted)
				continue
			}

			var itemBytes []byte
			err := it.Item().Value(func(val []byte) error {
				itemBytes = append([]byte{}, val...)
				return nil
			})
			if err != nil {
				return err
			}

			// Unmarshal to get enqueued time
			var storedItem struct {
				Data       []byte    `json:"data"`
				EnqueuedAt time.Time `json:"enqueued_at"`
			}
			if err := json.Unmarshal(itemBytes, &storedItem); err != nil {
				// Fall back to treating whole value as data
				storedItem.Data = itemBytes
				storedItem.EnqueuedAt = time.Now()
			}

			items = append(items, QueueItem{
				Offset:     offset,
				Data:       storedItem.Data,
				EnqueuedAt: storedItem.EnqueuedAt,
			})

			count++
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("queue: dequeue batch failed: %w", err)
	}

	if len(items) > 0 {
		q.dequeuedCount.Add(uint64(len(items)))
	}

	return items, nil
}

// Commit marks items up to and including the given offset as successfully processed
func (q *BadgerQueue) Commit(ctx context.Context, offset uint64) error {
	readOffset := q.readOffset.Load()

	if offset < readOffset {
		return ErrInvalidOffset
	}

	// Delete committed entries from disk
	err := q.db.Update(func(txn *badger.Txn) error {
		for o := readOffset; o <= offset; o++ {
			key := formatKey(q.cfg.Stream, o)
			if err := txn.Delete(key); err != nil && err != badger.ErrKeyNotFound {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("queue: commit failed: %w", err)
	}

	// Advance read pointer
	q.readOffset.Store(offset + 1)
	committedCount := offset - readOffset + 1
	q.committedCount.Add(committedCount)

	return nil
}

// MoveToDLQ moves an item to the dead letter queue
func (q *BadgerQueue) MoveToDLQ(ctx context.Context, item QueueItem, reason string, err error) error {
	if q.dlq == nil {
		return fmt.Errorf("queue: DLQ not configured")
	}

	if q.cfg.Stream == StreamDLQ {
		return fmt.Errorf("queue: cannot move DLQ item to DLQ")
	}

	dlqEntry := DLQEntry{
		OriginalEvent:  item.Data,
		Stream:         q.cfg.Stream,
		Reason:         reason,
		ErrorMessage:   err.Error(),
		FirstAttemptAt: item.EnqueuedAt,
		LastAttemptAt:  time.Now().UTC(),
		AttemptCount:   1,
	}

	data, marshalErr := json.Marshal(dlqEntry)
	if marshalErr != nil {
		return fmt.Errorf("queue: failed to marshal DLQ entry: %w", marshalErr)
	}

	// Enqueue to DLQ
	if err := q.dlq.Enqueue(ctx, data); err != nil {
		return fmt.Errorf("queue: failed to enqueue to DLQ: %w", err)
	}

	// Commit the original item (remove from main queue)
	if err := q.Commit(ctx, item.Offset); err != nil {
		q.logger.Warn("failed to commit item after moving to DLQ",
			"stream", q.cfg.Stream,
			"offset", item.Offset,
			"error", err)
	}

	return nil
}

// Metrics returns current queue metrics
func (q *BadgerQueue) Metrics() Metrics {
	depth := q.depth()
	bytesUsed := q.approximateBytesUsed()

	return Metrics{
		Depth:          depth,
		DLQSize:        0, // Only DLQ queue has non-zero value
		DroppedCount:   q.droppedCount.Load(),
		EnqueuedCount:  q.enqueuedCount.Load(),
		DequeuedCount:  q.dequeuedCount.Load(),
		CommittedCount: q.committedCount.Load(),
		BytesUsed:      bytesUsed,
	}
}

// Close gracefully closes the queue
func (q *BadgerQueue) Close() error {
	if !q.closed.CompareAndSwap(false, true) {
		return nil // Already closed
	}

	q.logger.Info("closing durable queue", "stream", q.cfg.Stream)

	return q.db.Close()
}

// depth returns the current number of items in the queue
func (q *BadgerQueue) depth() int {
	writeOffset := q.writeOffset.Load()
	readOffset := q.readOffset.Load()

	if writeOffset <= readOffset {
		return 0
	}

	return int(writeOffset - readOffset)
}

// checkCapacity checks if the queue has reached capacity limits
func (q *BadgerQueue) checkCapacity() error {
	depth := q.depth()

	// Check event count limit
	if int64(depth) >= q.cfg.MaxEvents {
		return ErrQueueFull
	}

	// Check byte limit (approximate)
	bytesUsed := q.approximateBytesUsed()
	if bytesUsed >= q.cfg.MaxBytes {
		return ErrQueueFull
	}

	return nil
}

// dropOldest removes the oldest item from the queue to make room
func (q *BadgerQueue) dropOldest() error {
	readOffset := q.readOffset.Load()
	writeOffset := q.writeOffset.Load()

	if readOffset >= writeOffset {
		return nil // Queue is empty
	}

	// Delete oldest entry
	key := formatKey(q.cfg.Stream, readOffset)

	err := q.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})

	if err != nil && err != badger.ErrKeyNotFound {
		return err
	}

	q.readOffset.Add(1)
	q.droppedCount.Add(1)

	return nil
}

// approximateBytesUsed estimates the disk space used by this queue
func (q *BadgerQueue) approximateBytesUsed() int64 {
	// BadgerDB provides LSM() stats but they're for the entire DB
	// For now, use a simple estimation: depth * average item size
	// In production, we'd track this more accurately

	depth := q.depth()
	if depth == 0 {
		return 0
	}

	// Estimate average item size (very rough)
	// A typical event is ~500 bytes
	const avgItemSize = 500

	return int64(depth) * avgItemSize
}

// recover rebuilds the queue state from disk after restart
func (q *BadgerQueue) recover(ctx context.Context) error {
	var minOffset uint64 = math.MaxUint64
	var maxOffset uint64 = 0
	count := 0

	err := q.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false // We only need keys for recovery

		it := txn.NewIterator(opts)
		defer it.Close()

		prefix := []byte(fmt.Sprintf("%s:", q.cfg.Stream))

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			offset, err := parseOffsetFromKey(it.Item().Key())
			if err != nil {
				q.logger.Warn("failed to parse offset from key",
					"key", string(it.Item().Key()),
					"error", err)
				continue
			}

			if offset < minOffset {
				minOffset = offset
			}
			if offset > maxOffset {
				maxOffset = offset
			}
			count++
		}

		return nil
	})

	if err != nil {
		return err
	}

	if count == 0 {
		// Empty queue - start from 0
		q.readOffset.Store(0)
		q.writeOffset.Store(0)
	} else {
		q.readOffset.Store(minOffset)
		q.writeOffset.Store(maxOffset + 1)
	}

	q.logger.Info("queue recovered from disk",
		"stream", q.cfg.Stream,
		"read_offset", q.readOffset.Load(),
		"write_offset", q.writeOffset.Load(),
		"depth", count)

	return nil
}

// runGC runs BadgerDB garbage collection periodically
func (q *BadgerQueue) runGC() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		if q.closed.Load() {
			return
		}

		<-ticker.C

		// Run value log GC (reclaims disk space)
		err := q.db.RunValueLogGC(0.5) // Discard 50%+ garbage
		if err != nil && err != badger.ErrNoRewrite {
			q.logger.Warn("value log GC failed",
				"stream", q.cfg.Stream,
				"error", err)
		}
	}
}
