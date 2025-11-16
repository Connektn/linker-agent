package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// DLQManager provides operations for managing the dead letter queue
type DLQManager struct {
	dlq *BadgerQueue
}

// NewDLQManager creates a new DLQ manager
func NewDLQManager(dlq *BadgerQueue) *DLQManager {
	return &DLQManager{dlq: dlq}
}

// ListEntries retrieves all entries from the DLQ
func (m *DLQManager) ListEntries(ctx context.Context, limit int) ([]DLQEntry, error) {
	if limit <= 0 {
		limit = 100 // Default limit
	}

	items, err := m.dlq.DequeueBatch(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("dlq: failed to list entries: %w", err)
	}

	entries := make([]DLQEntry, 0, len(items))
	for _, item := range items {
		var entry DLQEntry
		if err := json.Unmarshal(item.Data, &entry); err != nil {
			// Skip malformed entries
			continue
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// Count returns the number of entries in the DLQ
func (m *DLQManager) Count() int {
	metrics := m.dlq.Metrics()
	return metrics.Depth
}

// Purge removes all entries from the DLQ
func (m *DLQManager) Purge(ctx context.Context) error {
	// Get all items
	items, err := m.dlq.DequeueBatch(ctx, int(m.dlq.cfg.MaxEvents))
	if err != nil {
		return fmt.Errorf("dlq: failed to dequeue for purge: %w", err)
	}

	if len(items) == 0 {
		return nil
	}

	// Commit all (delete from disk)
	lastOffset := items[len(items)-1].Offset
	if err := m.dlq.Commit(ctx, lastOffset); err != nil {
		return fmt.Errorf("dlq: failed to commit purge: %w", err)
	}

	return nil
}

// PurgeOlderThan removes DLQ entries older than the specified duration
func (m *DLQManager) PurgeOlderThan(ctx context.Context, age time.Duration) (int, error) {
	cutoff := time.Now().Add(-age)
	purgedCount := 0

	// Get batches and delete old ones
	for {
		items, err := m.dlq.DequeueBatch(ctx, 100)
		if err != nil {
			return purgedCount, fmt.Errorf("dlq: failed to dequeue batch: %w", err)
		}

		if len(items) == 0 {
			break
		}

		for _, item := range items {
			var entry DLQEntry
			if err := json.Unmarshal(item.Data, &entry); err != nil {
				// Can't parse - skip
				continue
			}

			if entry.FirstAttemptAt.Before(cutoff) {
				// Delete this entry
				if err := m.dlq.Commit(ctx, item.Offset); err != nil {
					return purgedCount, fmt.Errorf("dlq: failed to delete old entry: %w", err)
				}
				purgedCount++
			}
		}

		// If we processed fewer items than requested, we're done
		if len(items) < 100 {
			break
		}
	}

	return purgedCount, nil
}

// Reprocess attempts to reprocess a DLQ entry by returning the original event
// The caller should re-enqueue this to the appropriate queue
func (m *DLQManager) Reprocess(ctx context.Context, offset uint64) ([]byte, Stream, error) {
	// Get single item
	items, err := m.dlq.DequeueBatch(ctx, 1)
	if err != nil {
		return nil, "", fmt.Errorf("dlq: failed to dequeue for reprocess: %w", err)
	}

	if len(items) == 0 {
		return nil, "", fmt.Errorf("dlq: no entry found at offset %d", offset)
	}

	item := items[0]
	if item.Offset != offset {
		return nil, "", fmt.Errorf("dlq: offset mismatch (wanted %d, got %d)", offset, item.Offset)
	}

	var entry DLQEntry
	if err := json.Unmarshal(item.Data, &entry); err != nil {
		return nil, "", fmt.Errorf("dlq: failed to unmarshal entry: %w", err)
	}

	// Remove from DLQ
	if err := m.dlq.Commit(ctx, offset); err != nil {
		return nil, "", fmt.Errorf("dlq: failed to commit after reprocess: %w", err)
	}

	return entry.OriginalEvent, entry.Stream, nil
}

// DLQStats returns statistics about the DLQ
type DLQStats struct {
	TotalEntries     int                `json:"total_entries"`
	OldestEntryAge   time.Duration      `json:"oldest_entry_age"`
	EntriesByReason  map[string]int     `json:"entries_by_reason"`
	EntriesByStream  map[Stream]int     `json:"entries_by_stream"`
}

// GetStats retrieves statistics about the DLQ
func (m *DLQManager) GetStats(ctx context.Context) (DLQStats, error) {
	stats := DLQStats{
		EntriesByReason: make(map[string]int),
		EntriesByStream: make(map[Stream]int),
	}

	// Get all entries (up to max)
	entries, err := m.ListEntries(ctx, int(m.dlq.cfg.MaxEvents))
	if err != nil {
		return stats, err
	}

	stats.TotalEntries = len(entries)

	if len(entries) == 0 {
		return stats, nil
	}

	// Find oldest entry
	oldest := time.Now()
	for _, entry := range entries {
		if entry.FirstAttemptAt.Before(oldest) {
			oldest = entry.FirstAttemptAt
		}

		stats.EntriesByReason[entry.Reason]++
		stats.EntriesByStream[entry.Stream]++
	}

	stats.OldestEntryAge = time.Since(oldest)

	return stats, nil
}
