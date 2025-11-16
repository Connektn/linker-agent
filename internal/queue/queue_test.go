package queue

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestBadgerQueue_EnqueueDequeue(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()

	cfg := Config{
		Stream:      StreamBilling,
		WALPath:     filepath.Join(tmpDir, "billing"),
		MaxBytes:    1024 * 1024, // 1MB
		MaxEvents:   100,
		DropPolicy:  DropOldest,
		SegmentSize: 0,
	}

	q, err := NewBadgerQueue(cfg, nil, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	ctx := context.Background()

	// Enqueue some items
	for i := 0; i < 10; i++ {
		data := []byte("test event " + string(rune('0'+i)))
		if err := q.Enqueue(ctx, data); err != nil {
			t.Fatalf("Failed to enqueue: %v", err)
		}
	}

	// Check metrics
	metrics := q.Metrics()
	if metrics.Depth != 10 {
		t.Errorf("Expected depth 10, got %d", metrics.Depth)
	}
	if metrics.EnqueuedCount != 10 {
		t.Errorf("Expected enqueued count 10, got %d", metrics.EnqueuedCount)
	}

	// Dequeue batch
	items, err := q.DequeueBatch(ctx, 5)
	if err != nil {
		t.Fatalf("Failed to dequeue: %v", err)
	}

	if len(items) != 5 {
		t.Errorf("Expected 5 items, got %d", len(items))
	}

	// Commit
	if err := q.Commit(ctx, items[len(items)-1].Offset); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

	// Check depth after commit
	metrics = q.Metrics()
	if metrics.Depth != 5 {
		t.Errorf("Expected depth 5 after commit, got %d", metrics.Depth)
	}
}

func TestBadgerQueue_Recovery(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Stream:      StreamUsage,
		WALPath:     filepath.Join(tmpDir, "usage"),
		MaxBytes:    1024 * 1024,
		MaxEvents:   100,
		DropPolicy:  DropOldest,
	}

	ctx := context.Background()

	// Create queue and enqueue items
	q1, err := NewBadgerQueue(cfg, nil, slog.Default())
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}

	for i := 0; i < 10; i++ {
		data := []byte("event " + string(rune('0'+i)))
		if err := q1.Enqueue(ctx, data); err != nil {
			t.Fatalf("Failed to enqueue: %v", err)
		}
	}

	// Close queue (simulates crash)
	q1.Close()

	// Reopen queue (recovery)
	q2, err := NewBadgerQueue(cfg, nil, slog.Default())
	if err != nil {
		t.Fatalf("Failed to recover queue: %v", err)
	}
	defer q2.Close()

	// Check that items were recovered
	metrics := q2.Metrics()
	if metrics.Depth != 10 {
		t.Errorf("Expected depth 10 after recovery, got %d", metrics.Depth)
	}

	// Verify we can dequeue recovered items
	items, err := q2.DequeueBatch(ctx, 10)
	if err != nil {
		t.Fatalf("Failed to dequeue after recovery: %v", err)
	}

	if len(items) != 10 {
		t.Errorf("Expected 10 recovered items, got %d", len(items))
	}
}

func TestBadgerQueue_RejectNew(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Stream:      StreamBilling,
		WALPath:     filepath.Join(tmpDir, "billing"),
		MaxBytes:    1024 * 1024,
		MaxEvents:   5, // Small limit
		DropPolicy:  RejectNew,
	}

	q, err := NewBadgerQueue(cfg, nil, slog.Default())
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	ctx := context.Background()

	// Fill queue to capacity
	for i := 0; i < 5; i++ {
		if err := q.Enqueue(ctx, []byte("event")); err != nil {
			t.Fatalf("Failed to enqueue %d: %v", i, err)
		}
	}

	// Try to enqueue one more (should fail)
	err = q.Enqueue(ctx, []byte("overflow"))
	if err != ErrQueueFull {
		t.Errorf("Expected ErrQueueFull, got %v", err)
	}

	// Verify no drops
	metrics := q.Metrics()
	if metrics.DroppedCount != 0 {
		t.Errorf("Expected 0 drops with reject_new, got %d", metrics.DroppedCount)
	}
}

func TestBadgerQueue_DropOldest(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Stream:      StreamUsage,
		WALPath:     filepath.Join(tmpDir, "usage"),
		MaxBytes:    1024 * 1024,
		MaxEvents:   5, // Small limit
		DropPolicy:  DropOldest,
	}

	q, err := NewBadgerQueue(cfg, nil, slog.Default())
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	ctx := context.Background()

	// Fill queue beyond capacity
	for i := 0; i < 10; i++ {
		if err := q.Enqueue(ctx, []byte("event")); err != nil {
			t.Fatalf("Failed to enqueue %d: %v", i, err)
		}
	}

	// Verify drops occurred
	metrics := q.Metrics()
	if metrics.DroppedCount == 0 {
		t.Error("Expected drops with drop_oldest policy")
	}

	// Verify depth is at max
	if metrics.Depth > 5 {
		t.Errorf("Expected depth <= 5, got %d", metrics.Depth)
	}
}

func TestMemoryQueue_Basic(t *testing.T) {
	q := NewMemoryQueue(StreamBilling, 10, DropOldest)
	defer q.Close()

	ctx := context.Background()

	// Enqueue items
	for i := 0; i < 5; i++ {
		if err := q.Enqueue(ctx, []byte("event")); err != nil {
			t.Fatalf("Failed to enqueue: %v", err)
		}
	}

	// Dequeue
	items, err := q.DequeueBatch(ctx, 3)
	if err != nil {
		t.Fatalf("Failed to dequeue: %v", err)
	}

	if len(items) != 3 {
		t.Errorf("Expected 3 items, got %d", len(items))
	}

	// Check metrics
	metrics := q.Metrics()
	if metrics.Depth != 2 {
		t.Errorf("Expected depth 2, got %d", metrics.Depth)
	}
}
