# Story 5 – Durable Queue Implementation with Disk Persistence

## Before you start
Read and follow all rules in `CLAUDE.md`.
This story implements production-grade durable queuing to ensure zero data loss during network outages and agent restarts.

---

## Goal
Replace the current **in-memory buffered channels** with **disk-backed durable queues** that:
1. Survive agent restarts and crashes without data loss
2. Buffer events during extended network outages (hours)
3. Support configurable capacity limits and drop policies
4. Provide separate queues per stream (billing, usage, edges)
5. Implement DLQ (Dead Letter Queue) for poison events

---

## Current Status

### ❌ Critical Limitations (Production Blocker)

The current implementation (`internal/exporter/queue.go`) uses **in-memory Go channels**:

**Data Loss Scenarios:**
1. ✗ Agent restart → all queued events lost
2. ✗ Agent crash → all queued events lost
3. ✗ Network outage + queue full → oldest events dropped silently
4. ✗ No persistence → no replay capability
5. ✗ No DLQ → poison events cause repeated failures

**From Code Analysis:**
- Line 12 in `queue.go`: `ch chan []byte` - buffered channel (RAM only)
- Lines 46-50: When full, drops oldest event (data loss)
- Line 96-98: DLQ not implemented (`dlqSize() returns 0`)
- No WAL, no RocksDB, no Badger, no SQLite

**Impact:**
- Cannot guarantee billing event delivery
- Network outage > queue capacity = data loss
- Violates "never lose billing events" principle

### ✅ What Works Today

1. **In-Memory Buffering**: Fast enqueue/dequeue with buffered channels
2. **Metrics**: Exposes `Depth()`, `DroppedCount()`, `EnqueuedCount()` via `QueueMetricsProvider`
3. **Graceful Shutdown**: Drains queue before exit
4. **HTTP Retry**: Exponential backoff for cloud endpoint failures

---

## Architecture Overview

### Three-Stage Data Path

```
┌─────────────────────────────────────────────────────────────┐
│ (1) INGEST                                                  │
│     - Stripe webhooks (billing events)                     │
│     - Gateway Adapter (usage/feature events)               │
│     - SDK events (future)                                  │
└─────────────────────────────────────────────────────────────┘
                        │
                        ▼ (non-blocking enqueue to disk)
┌─────────────────────────────────────────────────────────────┐
│ (2) DURABLE QUEUES (disk-backed, per stream)               │
│     ┌─────────────────────────────────────────┐            │
│     │ Billing Queue (Stripe events)           │            │
│     │ - WAL segments: billing-00001.log, ...  │            │
│     │ - Policy: reject_new (rely on Stripe)   │            │
│     └─────────────────────────────────────────┘            │
│     ┌─────────────────────────────────────────┐            │
│     │ Usage Queue (feature/analytics)         │            │
│     │ - WAL segments: usage-00001.log, ...    │            │
│     │ - Policy: drop_oldest (metrics-like)    │            │
│     └─────────────────────────────────────────┘            │
│     ┌─────────────────────────────────────────┐            │
│     │ Edges Queue (identity links)            │            │
│     │ - WAL segments: edges-00001.log, ...    │            │
│     │ - Policy: drop_oldest                   │            │
│     └─────────────────────────────────────────┘            │
│     ┌─────────────────────────────────────────┐            │
│     │ DLQ (poison/invalid events)             │            │
│     │ - Reason codes: validation_error, etc.  │            │
│     └─────────────────────────────────────────┘            │
└─────────────────────────────────────────────────────────────┘
                        │
                        ▼ (async workers; batch + backoff)
┌─────────────────────────────────────────────────────────────┐
│ (3) EXPORTER → Connektn Cloud                              │
│     - POST https://api.connektn.io/ingest                  │
│     - Retries, exponential backoff, rate limiting          │
│     - Acknowledge success → advance read offset            │
└─────────────────────────────────────────────────────────────┘
```

**Resilience Rule:** Network problems only affect stage (3). Stages (1) and (2) continue working as long as there is local disk capacity.

---

## Requirements

### 1. Disk-Backed Queue Storage

**Option A: Embedded Key-Value Store (Recommended)**

Use **BadgerDB** or **Pebble** (RocksDB-like embedded DB):

```go
// Example structure
type DurableQueue struct {
    db          *badger.DB
    stream      Stream
    writeOffset uint64
    readOffset  uint64
    metrics     QueueMetrics
}
```

**Why BadgerDB/Pebble:**
- Pure Go (no CGO dependencies)
- LSM-tree structure (fast writes)
- Built-in WAL and compaction
- Production-proven (used by Dgraph, IPFS, etc.)
- Crash recovery built-in

**Option B: Append-Only Log Segments**

Custom WAL implementation:

```
/var/lib/connektn/wal/
  billing/
    00001.log  (segment 1, max 100MB)
    00002.log  (segment 2, max 100MB)
    metadata.json (write/read offsets)
  usage/
    00001.log
    metadata.json
  edges/
    00001.log
    metadata.json
```

**Trade-offs:**
- Option A (BadgerDB): Less code, better tested, more features
- Option B (Custom WAL): More control, simpler on-disk format

**Recommendation:** Use **BadgerDB** for faster implementation and better reliability.

### 2. Per-Stream Queue Configuration

```yaml
export:
  mode: "cloud"
  cloud:
    endpoint: "https://api.connektn.io/ingest"
    timeout: 30s
    maxRetries: 5
    batchSize: 100
    flushInterval: 5s

queue:
  type: "durable"               # "memory" (current) | "durable" (new)
  walPath: "/var/lib/connektn/wal"

  # Per-stream configuration
  billing:
    maxBytes: 5368709120        # 5GB max disk usage
    maxEvents: 1000000          # 1M events max
    dropPolicy: "reject_new"    # "drop_oldest" | "reject_new"
    segmentSize: 104857600      # 100MB per segment

  usage:
    maxBytes: 2147483648        # 2GB max
    maxEvents: 5000000          # 5M events max
    dropPolicy: "drop_oldest"   # OK to lose old metrics
    segmentSize: 104857600

  edges:
    maxBytes: 1073741824        # 1GB max
    maxEvents: 500000
    dropPolicy: "drop_oldest"
    segmentSize: 104857600

  dlq:
    maxBytes: 536870912         # 500MB max
    retentionDays: 7            # Auto-cleanup after 7 days
```

### 3. Queue Operations

**Enqueue (Write Path):**
```go
// Non-blocking write to disk
func (q *DurableQueue) Enqueue(ctx context.Context, data []byte) error {
    // 1. Check capacity limits
    if q.isFull() {
        return q.applyDropPolicy(data)
    }

    // 2. Write to disk (BadgerDB transaction)
    txn := q.db.NewTransaction(true)
    defer txn.Discard()

    key := formatKey(q.stream, q.writeOffset)
    if err := txn.Set(key, data); err != nil {
        return err
    }

    if err := txn.Commit(); err != nil {
        return err
    }

    // 3. Update metrics
    q.writeOffset++
    q.metrics.EnqueuedCount++
    q.metrics.Depth = int(q.writeOffset - q.readOffset)

    return nil
}
```

**Dequeue (Read Path):**
```go
// Batch read for exporter worker
func (q *DurableQueue) DequeueBatch(ctx context.Context, batchSize int) ([]QueueItem, error) {
    items := make([]QueueItem, 0, batchSize)

    txn := q.db.NewTransaction(false) // read-only
    defer txn.Discard()

    for i := 0; i < batchSize && q.readOffset < q.writeOffset; i++ {
        key := formatKey(q.stream, q.readOffset+uint64(i))
        item, err := txn.Get(key)
        if err != nil {
            return nil, err
        }

        var data []byte
        err = item.Value(func(val []byte) error {
            data = append([]byte{}, val...)
            return nil
        })
        if err != nil {
            return nil, err
        }

        items = append(items, QueueItem{
            Offset: q.readOffset + uint64(i),
            Data:   data,
        })
    }

    return items, nil
}
```

**Commit (Acknowledge Success):**
```go
// Called after successful HTTP export
func (q *DurableQueue) Commit(ctx context.Context, offset uint64) error {
    // Delete committed entries from disk
    txn := q.db.NewTransaction(true)
    defer txn.Discard()

    for o := q.readOffset; o <= offset; o++ {
        key := formatKey(q.stream, o)
        if err := txn.Delete(key); err != nil {
            return err
        }
    }

    if err := txn.Commit(); err != nil {
        return err
    }

    // Advance read pointer
    q.readOffset = offset + 1
    q.metrics.Depth = int(q.writeOffset - q.readOffset)

    return nil
}
```

### 4. Drop Policies

**Drop Oldest (for usage/edges queues):**
```go
func (q *DurableQueue) applyDropOldest(newData []byte) error {
    // 1. Delete oldest entry
    txn := q.db.NewTransaction(true)
    defer txn.Discard()

    oldestKey := formatKey(q.stream, q.readOffset)
    if err := txn.Delete(oldestKey); err != nil {
        return err
    }

    // 2. Write new entry
    newKey := formatKey(q.stream, q.writeOffset)
    if err := txn.Set(newKey, newData); err != nil {
        return err
    }

    if err := txn.Commit(); err != nil {
        return err
    }

    // 3. Update offsets and metrics
    q.readOffset++
    q.writeOffset++
    q.metrics.DroppedCount++

    return nil
}
```

**Reject New (for billing queue):**
```go
func (q *DurableQueue) applyRejectNew(newData []byte) error {
    // Return error to webhook handler
    // Handler will return HTTP 503/429 to Stripe
    // Stripe will retry later
    return ErrQueueFull
}
```

### 5. Dead Letter Queue (DLQ)

**When to DLQ an Event:**
- JSON schema validation fails
- Unknown organization ID
- Malformed data (not recoverable)
- Export fails with 4xx (client error, not network issue)

**DLQ Entry Format:**
```go
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
```

**DLQ Operations:**
```go
func (q *DurableQueue) MoveToDLQ(ctx context.Context, item QueueItem, reason string, err error) error {
    dlqEntry := DLQEntry{
        OriginalEvent:  item.Data,
        Stream:         q.stream,
        Reason:         reason,
        ErrorMessage:   err.Error(),
        FirstAttemptAt: time.Now(),
        LastAttemptAt:  time.Now(),
        AttemptCount:   1,
    }

    // Write to DLQ (separate BadgerDB instance or prefix)
    data, _ := json.Marshal(dlqEntry)
    return q.dlq.Enqueue(ctx, data)
}
```

### 6. Network Outage Behavior

**Detection:**
```go
// Exporter worker detects network issues
func (e *Exporter) isNetworkError(err error) bool {
    // DNS failure, connection refused, TLS handshake failure
    if netErr, ok := err.(net.Error); ok {
        return netErr.Timeout() || netErr.Temporary()
    }

    // HTTP 5xx or persistent 429
    if httpErr, ok := err.(*HTTPError); ok {
        return httpErr.StatusCode >= 500 || httpErr.StatusCode == 429
    }

    return false
}
```

**Backoff Strategy:**
```go
type BackoffSchedule struct {
    attempts int
    delays   []time.Duration // [1s, 2s, 5s, 10s, 30s, 60s]
}

func (e *Exporter) exportWithBackoff(ctx context.Context, batch []QueueItem) error {
    backoff := &BackoffSchedule{
        delays: []time.Duration{
            1 * time.Second,
            2 * time.Second,
            5 * time.Second,
            10 * time.Second,
            30 * time.Second,
            60 * time.Second, // max backoff
        },
    }

    for {
        err := e.sendBatch(ctx, batch)

        if err == nil {
            // Success - commit offsets
            return nil
        }

        if !e.isNetworkError(err) {
            // Not a network error - move to DLQ
            return e.moveBatchToDLQ(batch, err)
        }

        // Network error - backoff and retry
        delay := backoff.next()

        e.logger.Warn("network error, backing off",
            "attempt", backoff.attempts,
            "delay", delay,
            "error", err)

        select {
        case <-time.After(delay):
            continue
        case <-ctx.Done():
            return ctx.Err()
        }
    }
}
```

**Heartbeat During Outage:**
```go
// Heartbeat payload shows queue health
{
  "agentId": "agent_xxx",
  "queueDepth": 125000,      // ← Growing during outage
  "dlqSize": 5,
  "droppedCount": 0,
  "retryCount": 47,          // ← Increasing with backoff attempts
  "timestamp": 1735998660
}
```

**Cloud Alerts:**
- `queueDepth > 10000 for 10 minutes` → network/cloud issue
- `droppedCount increasing` → queue capacity problem
- `dlqSize > 100` → data quality or configuration issue

### 7. Crash Recovery & Restart

**On Agent Start:**
```go
func (q *DurableQueue) Recover(ctx context.Context) error {
    // 1. Open BadgerDB (automatically replays WAL)
    opts := badger.DefaultOptions(q.walPath)
    db, err := badger.Open(opts)
    if err != nil {
        return fmt.Errorf("queue recovery: %w", err)
    }
    q.db = db

    // 2. Rebuild in-memory index (scan for min/max offsets)
    txn := db.NewTransaction(false)
    defer txn.Discard()

    it := txn.NewIterator(badger.DefaultIteratorOptions)
    defer it.Close()

    prefix := []byte(fmt.Sprintf("%s:", q.stream))
    minOffset := uint64(math.MaxUint64)
    maxOffset := uint64(0)
    count := 0

    for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
        offset := parseOffsetFromKey(it.Item().Key())
        if offset < minOffset {
            minOffset = offset
        }
        if offset > maxOffset {
            maxOffset = offset
        }
        count++
    }

    // 3. Set offsets
    q.readOffset = minOffset
    q.writeOffset = maxOffset + 1
    q.metrics.Depth = count

    q.logger.Info("queue recovered from disk",
        "stream", q.stream,
        "read_offset", q.readOffset,
        "write_offset", q.writeOffset,
        "depth", count)

    return nil
}
```

**Startup Sequence:**
```
1. Load config
2. Recover queues from disk (billing, usage, edges, dlq)
3. Start exporter workers (resume from last committed offset)
4. Start webhook handlers (begin accepting new events)
```

### 8. Stripe Webhook Integration

**Billing Queue Full → Rely on Stripe Retries:**
```go
// In webhook handler
func (h *Handler) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
    // ... signature verification ...

    event := parseStripeEvent(r.Body)

    // Enqueue to billing queue
    err := h.billingQueue.Enqueue(r.Context(), event)

    if errors.Is(err, ErrQueueFull) {
        // Queue full - return 503 to Stripe
        // Stripe will retry (exponential backoff, up to 3 days)
        h.logger.Warn("billing queue full, rejecting webhook",
            "event_id", event.ID)

        w.WriteHeader(http.StatusServiceUnavailable)
        w.Header().Set("Retry-After", "300") // 5 minutes
        return
    }

    if err != nil {
        // Other error - log but still return 200 to avoid Stripe retry storm
        h.logger.Error("failed to enqueue webhook",
            "event_id", event.ID,
            "error", err)
    }

    // Success
    w.WriteHeader(http.StatusOK)
}
```

**Stripe Retry Behavior:**
- Initial retry: after 5 minutes
- Subsequent: exponential backoff (15m, 1h, 3h, 6h, 12h, 24h)
- Max retention: 3 days
- Webhook visible in Stripe dashboard with retry status

---

## Code Structure

```
internal/queue/
  ├─ queue.go              # Core durable queue interface
  ├─ badger_queue.go       # BadgerDB implementation
  ├─ dlq.go                # Dead letter queue logic
  ├─ metrics.go            # Queue metrics (QueueMetricsProvider)
  ├─ recovery.go           # Crash recovery logic
  ├─ queue_test.go         # Unit tests
  └─ integration_test.go   # Integration tests with disk

internal/exporter/
  ├─ exporter.go           # Updated to use durable queues
  ├─ worker.go             # Per-stream export workers
  ├─ backoff.go            # Exponential backoff logic
  └─ exporter_test.go      # Updated tests

internal/config/
  └─ config.go             # Add queue configuration structs

cmd/linker-agent/
  └─ dlq.go                # NEW: DLQ CLI commands
                           # - dlq list
                           # - dlq reprocess
                           # - dlq delete
```

---

## Acceptance Criteria

1. ✅ **Zero Data Loss on Restart**: Agent restart preserves all queued events
2. ✅ **Network Outage Resilience**: Queue continues accepting events during multi-hour network outages
3. ✅ **Configurable Capacity**: Per-stream `maxBytes` and `maxEvents` enforced
4. ✅ **Drop Policies Work**: `drop_oldest` and `reject_new` behave correctly
5. ✅ **DLQ Functional**: Poison events move to DLQ with reason codes
6. ✅ **Stripe Integration**: Billing queue full → returns 503 to Stripe (triggers retry)
7. ✅ **Metrics Accurate**: `Depth()`, `DLQSize()`, `DroppedCount()`, `EnqueuedCount()` reflect disk state
8. ✅ **Crash Recovery**: Corrupted DB recovers gracefully (BadgerDB handles this)
9. ✅ **Performance**: Enqueue <1ms p95, no memory growth during backlog
10. ✅ **CLI Tools**: `dlq list` and `dlq reprocess` commands work

---

## Test Plan

### Unit Tests
- Queue enqueue/dequeue operations
- Drop policy logic (drop_oldest, reject_new)
- Offset management and commit logic
- DLQ reason code assignment

### Integration Tests

**1. Network Outage Simulation:**
```go
func TestQueueDuringNetworkOutage(t *testing.T) {
    // 1. Start agent with durable queue
    agent := startAgent(t, durableQueueConfig)

    // 2. Enqueue 1000 events
    for i := 0; i < 1000; i++ {
        agent.EnqueueBillingEvent(makeFakeEvent(i))
    }

    // 3. Simulate network down (mock cloud endpoint returns error)
    agent.SimulateNetworkDown()

    // 4. Enqueue 10,000 more events (queue should accept them)
    for i := 0; i < 10000; i++ {
        agent.EnqueueBillingEvent(makeFakeEvent(1000 + i))
    }

    // 5. Verify queue depth = 11,000
    assert.Equal(t, 11000, agent.QueueDepth("billing"))

    // 6. Restore network
    agent.SimulateNetworkUp()

    // 7. Wait for export
    waitForQueueDrain(t, agent, 60*time.Second)

    // 8. Verify all events exported
    assert.Equal(t, 0, agent.QueueDepth("billing"))
    assert.Equal(t, 11000, agent.ExportedCount("billing"))
}
```

**2. Crash Recovery:**
```go
func TestQueueCrashRecovery(t *testing.T) {
    // 1. Enqueue events
    agent := startAgent(t, durableQueueConfig)
    agent.EnqueueBillingEvents(1000)

    // 2. Simulate crash (kill -9)
    agent.Kill()

    // 3. Restart agent
    agent = startAgent(t, durableQueueConfig)

    // 4. Verify queue recovered
    assert.Equal(t, 1000, agent.QueueDepth("billing"))

    // 5. Verify export resumes
    waitForQueueDrain(t, agent, 30*time.Second)
    assert.Equal(t, 1000, agent.ExportedCount("billing"))
}
```

**3. Queue Full Behavior:**
```go
func TestQueueFullRejectNew(t *testing.T) {
    // 1. Configure small queue (maxEvents: 100)
    cfg := durableQueueConfig
    cfg.Queue.Billing.MaxEvents = 100
    cfg.Queue.Billing.DropPolicy = "reject_new"

    agent := startAgent(t, cfg)

    // 2. Simulate network down
    agent.SimulateNetworkDown()

    // 3. Enqueue 100 events (should succeed)
    for i := 0; i < 100; i++ {
        err := agent.EnqueueBillingEvent(makeFakeEvent(i))
        assert.NoError(t, err)
    }

    // 4. Enqueue 101st event (should fail with ErrQueueFull)
    err := agent.EnqueueBillingEvent(makeFakeEvent(100))
    assert.ErrorIs(t, err, queue.ErrQueueFull)

    // 5. Verify dropped count = 0 (rejected, not dropped)
    assert.Equal(t, uint64(0), agent.DroppedCount("billing"))
}
```

**4. DLQ for Poison Events:**
```go
func TestDLQPoisonEvents(t *testing.T) {
    agent := startAgent(t, durableQueueConfig)

    // 1. Enqueue valid event
    agent.EnqueueBillingEvent(validEvent)

    // 2. Enqueue invalid event (bad schema)
    agent.EnqueueBillingEvent(invalidSchemaEvent)

    // 3. Wait for export
    time.Sleep(2 * time.Second)

    // 4. Verify valid event exported
    assert.Equal(t, 1, agent.ExportedCount("billing"))

    // 5. Verify invalid event in DLQ
    assert.Equal(t, 1, agent.DLQSize())

    // 6. Check DLQ entry has reason
    dlqEntries := agent.ListDLQ()
    assert.Equal(t, "validation_error", dlqEntries[0].Reason)
}
```

### Performance Tests
- **Throughput**: Enqueue 100k events/sec sustained
- **Memory**: No memory growth during 1M event backlog
- **Disk Usage**: Compaction works, old segments deleted
- **Latency**: p95 enqueue <1ms, p99 <5ms

### Stress Tests
- Fill queue to maxBytes, verify cap enforced
- Simulate 24-hour network outage with continuous ingest
- Rapid restart cycles (10 restarts in 1 minute)

---

## Deliverables

1. **Code**:
   - `internal/queue/` package with BadgerDB implementation
   - `internal/exporter/` updated to use durable queues
   - CLI commands: `dlq list`, `dlq reprocess`, `dlq delete`

2. **Configuration**:
   - Updated `config.yaml` with `queue:` section
   - Per-stream queue configuration
   - Environment variable overrides

3. **Documentation**:
   - `/docs/queues.md` - architecture and operation guide
   - `/docs/recovery.md` - crash recovery procedures
   - `/docs/dlq.md` - DLQ management guide
   - Update README.md with queue configuration examples

4. **Tests**:
   - Unit tests (90%+ coverage)
   - Integration tests (network outage, crash, queue full, DLQ)
   - Performance benchmarks

5. **Metrics**:
   - Prometheus metrics for queue depth, DLQ size, dropped count
   - Grafana dashboard template (optional)

6. **Migration Guide**:
   - How to migrate from in-memory to durable queues
   - Zero-downtime upgrade procedure

---

## Dependencies

- **BadgerDB** (or Pebble): `github.com/dgraph-io/badger/v4`
- Existing exporter infrastructure
- Existing metrics/heartbeat system
- Stripe webhook handler

---

## Non-Goals (Out of Scope)

- ❌ Distributed queue (Kafka/RabbitMQ) - see Story 3 from Sprint 1
- ❌ Queue replication/HA - single-node only
- ❌ Queue encryption at rest (can be Phase 2)
- ❌ Queue compression (BadgerDB has built-in compression)
- ❌ Custom serialization format (use JSON)

---

## Security & Privacy Considerations

1. **No PII in Queue**:
   - Events already sanitized before enqueue
   - Queue stores synthetic IDs only
   - Tenant salt never written to queue

2. **Disk Permissions**:
   - WAL directory: `chmod 700` (owner only)
   - DB files: `chmod 600`
   - No world-readable files

3. **Crash Safety**:
   - BadgerDB provides ACID guarantees
   - Fsync on commit (configurable)
   - Automatic WAL replay on recovery

4. **DLQ Retention**:
   - Auto-delete after `retentionDays`
   - Never indefinitely accumulate poison events
   - Manual cleanup via CLI

---

## Migration Path

**Phase 1: Development & Testing**
- Implement `internal/queue/` with feature flag
- Add `queue.type: "memory" | "durable"` config
- Default to `memory` (current behavior)
- Test in staging with `durable` enabled

**Phase 2: Gradual Rollout**
- Enable `durable` for new agent installations
- Provide migration script for existing agents
- Monitor queue metrics during migration

**Phase 3: Deprecate In-Memory**
- Remove in-memory queue code
- Make `durable` the only option
- Update documentation

---

## Future Enhancements (Phase 2)

1. **Queue Compression**:
   - Enable BadgerDB compression (Snappy/Zstd)
   - Reduce disk usage by 50-70%

2. **Queue Encryption**:
   - Encrypt queue data at rest
   - Use tenant-provided encryption key

3. **Multi-Tenancy**:
   - Separate queues per organization
   - Quota enforcement per org

4. **Advanced DLQ**:
   - DLQ retry scheduler (retry after X hours)
   - DLQ event editing (fix and reprocess)
   - DLQ analytics dashboard

5. **Queue Monitoring**:
   - Real-time queue depth graphs
   - Alerts for queue saturation
   - Export queue metrics to cloud

---

**Author:** Tomas Zezula
**Status:** Backlog
**Priority:** Critical (Production Blocker)
**Estimated Effort:** 5-7 days
**Depends On:** None (can start immediately)
**Blocks:** Production deployment
