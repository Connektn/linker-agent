# Story 4 – Stripe Bulk Import for Initial Onboarding

## Before you start
Read and follow all rules in `CLAUDE.md`.
This story extends the existing Stripe connector to support full bulk data import during initial agent installation.

---

## Goal
Enable **initial bulk import** of all Stripe data (customers, subscriptions, invoices, payment methods) when the Linker Agent is first installed, before transitioning to real-time webhook mode.

This provides:
1. Complete historical data baseline for matching and analytics
2. Immediate value for new customers without waiting for webhook events
3. Proper reconciliation foundation before incremental updates begin

---

## Current Status

### ✅ Already Implemented

The Stripe connector (`internal/connectors/stripe/`) provides:

1. **Basic List Methods** (lines 129-385 in `stripe.go`):
   - `ListSubscriptions(ctx, customerID, limit)` - sanitized subscriptions
   - `ListRawSubscriptions(ctx, customerID, limit)` - raw Stripe objects for synthetic ID generation
   - `ListInvoices(ctx, customerID, subscriptionID, limit)` - sanitized invoices
   - `ListRawInvoices(ctx, customerID, subscriptionID, limit)` - raw Stripe objects

2. **Rate Limiting** (lines 444-502):
   - Token bucket rate limiter (in-memory)
   - Configurable max RPS
   - Context-aware blocking

3. **Stripe Connect Support**:
   - Optional account parameter for multi-tenant scenarios
   - Proper header propagation

4. **Current Usage in main.go**:
   - `runMatcherPipeline()`: fetches 100 subscriptions + 100 invoices
   - `runBillingExportMode()`: same limits
   - Fixed limit, no pagination beyond first page

### ❌ Missing for Full Bulk Import

1. **No Auto-Pagination**:
   - Current implementation stops after `limit` records
   - Stripe SDK iterator supports pagination, but we only fetch first page
   - Accounts with >100 subscriptions/invoices would be incomplete

2. **No Customer Listing**:
   - Only `SmokeCheck()` lists 5 customers for health checks
   - No `ListRawCustomers()` for bulk import
   - Customer metadata (created timestamp, payment methods) needed for enrichment

3. **Missing Stripe Objects**:
   - **Payment Methods** - needed for payment failure correlation
   - **Charges** - for detailed payment history
   - **Products** - for SKU/feature matching (currently inferred from subscription items)
   - **Prices** - for pricing tier analysis

4. **No Progress Tracking**:
   - Large imports (10k+ records) have no progress visibility
   - No checkpoint/resume capability for failed imports
   - No metrics on import duration or throughput

5. **No Backfill Mode**:
   - No flag or configuration to distinguish initial import from webhook mode
   - No separate command like `--backfill` or `--initial-import`

6. **Rate Limit Challenges**:
   - Stripe rate limits: 100 req/sec (read), lower for test mode
   - Current simple rate limiter may be too aggressive or too lenient
   - No backoff strategy for 429 responses

---

## Requirements

### 1. Auto-Pagination Support

Add methods that fetch **all** records with automatic pagination:

```go
// ListAllRawSubscriptions fetches all subscriptions using cursor-based pagination
func (c *Client) ListAllRawSubscriptions(ctx context.Context, opts BulkImportOptions) ([]*stripe.Subscription, error)

// ListAllRawInvoices fetches all invoices using cursor-based pagination
func (c *Client) ListAllRawInvoices(ctx context.Context, opts BulkImportOptions) ([]*stripe.Invoice, error)

// ListAllRawCustomers fetches all customers using cursor-based pagination
func (c *Client) ListAllRawCustomers(ctx context.Context, opts BulkImportOptions) ([]*stripe.Customer, error)
```

**Options struct:**
```go
type BulkImportOptions struct {
    PageSize         int                  // Records per API call (default 100, max 100)
    MaxRecords       int                  // Stop after N records (0 = unlimited)
    ProgressCallback func(int, int)       // Callback: progressCallback(fetched, total)
    RateLimitRPS     int                  // Override default rate limit
    CreatedAfter     int64                // Unix timestamp filter (optional)
    CreatedBefore    int64                // Unix timestamp filter (optional)
}
```

### 2. Customer Data Import

Add customer listing with relevant fields (no PII):

```go
type Customer struct {
    ID              string // Stripe customer ID
    CreatedAt       int64  // Unix timestamp
    DefaultSource   string // Default payment method ID (opaque)
    InvoiceSettings struct {
        DefaultPaymentMethod string
    }
    // NO email, name, address, phone, description
}

func (c *Client) ListAllRawCustomers(ctx context.Context, opts BulkImportOptions) ([]*stripe.Customer, error)
```

### 3. Additional Stripe Objects (Optional Phase 2)

For comprehensive billing analysis:

```go
// Payment Methods - for payment failure correlation
func (c *Client) ListAllRawPaymentMethods(ctx context.Context, customerID string, opts BulkImportOptions) ([]*stripe.PaymentMethod, error)

// Products - for SKU/feature mapping
func (c *Client) ListAllRawProducts(ctx context.Context, opts BulkImportOptions) ([]*stripe.Product, error)

// Prices - for pricing tier analysis
func (c *Client) ListAllRawPrices(ctx context.Context, opts BulkImportOptions) ([]*stripe.Price, error)
```

### 4. Backfill Command Mode

Add new CLI flag and execution mode:

```bash
./linker-agent --backfill [--since=YYYY-MM-DD]
```

**Behavior:**
1. Fetch all Stripe data (customers, subscriptions, invoices)
2. Process through matcher pipeline with synthetic ID generation
3. Export to configured sinks (file/HTTP)
4. Report summary: records fetched, matches found, export status
5. Exit cleanly (not long-running like webhook mode)

**Configuration:**
```yaml
backfill:
  enabled: false                    # Safety: must be explicitly enabled
  pageSize: 100                     # Stripe API page size
  maxConcurrentRequests: 5          # Parallel fetching limit
  checkpointFile: /var/lib/connektn/backfill-checkpoint.json
  since: "2024-01-01T00:00:00Z"     # Optional: only import data after this timestamp
```

### 5. Progress Tracking & Metrics

Add structured logging and metrics:

```go
type ImportProgress struct {
    StartTime          time.Time
    CustomersTotal     int
    CustomersFetched   int
    SubscriptionsTotal int
    SubscriptionsFetched int
    InvoicesTotal      int
    InvoicesFetched    int
    Errors             []ImportError
}
```

**Logs:**
```
2025-01-15 10:00:00 INFO  Starting Stripe bulk import (backfill mode)
2025-01-15 10:00:05 INFO  Fetched 100/2500 customers (4%)
2025-01-15 10:00:10 INFO  Fetched 500/2500 customers (20%)
...
2025-01-15 10:05:00 INFO  Completed customers: 2500 fetched, 0 errors
2025-01-15 10:05:00 INFO  Starting subscriptions import...
2025-01-15 10:08:00 INFO  Completed: 2500 customers, 1200 subscriptions, 8500 invoices
2025-01-15 10:08:00 INFO  Export: 15000 billing records, 3200 link edges
```

**Prometheus Metrics:**
```
connektn_backfill_records_fetched{object_type="customer|subscription|invoice"}
connektn_backfill_duration_seconds{phase="fetch|process|export"}
connektn_backfill_errors_total{object_type="...", error_type="rate_limit|timeout|api_error"}
```

### 6. Checkpoint & Resume

For large imports that may fail mid-way:

```json
// /var/lib/connektn/backfill-checkpoint.json
{
  "started_at": "2025-01-15T10:00:00Z",
  "customers_cursor": "cus_abc123",
  "subscriptions_cursor": "sub_def456",
  "invoices_cursor": "in_xyz789",
  "status": "in_progress"
}
```

**Resume logic:**
- If checkpoint exists and status is `in_progress`, resume from cursors
- If `--force` flag provided, ignore checkpoint and restart
- On completion, update checkpoint status to `completed`

### 7. Rate Limiting Enhancements

Improve rate limiter for bulk operations:

1. **Adaptive Rate Limiting**:
   - Detect 429 responses from Stripe
   - Exponential backoff: wait 1s, 2s, 4s, 8s, 16s (max)
   - Resume at reduced rate after backoff

2. **Burst Handling**:
   - Allow bursts up to 2x max RPS for short periods
   - Track 1-second and 1-minute windows

3. **Monitoring**:
   - Log rate limit hits
   - Expose metrics: `connektn_stripe_rate_limit_hits_total`

---

## Architecture

### Execution Flow (Backfill Mode)

```
┌─────────────────────────────────────────────────────┐
│ 1. CLI: ./linker-agent --backfill                  │
└─────────────────────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────┐
│ 2. Load config, check if backfill.enabled=true     │
└─────────────────────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────┐
│ 3. Check for checkpoint file (resume if exists)    │
└─────────────────────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────┐
│ 4. Fetch all customers (paginate, track progress)  │
└─────────────────────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────┐
│ 5. Fetch all subscriptions (paginate)              │
└─────────────────────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────┐
│ 6. Fetch all invoices (paginate)                   │
└─────────────────────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────┐
│ 7. Sanitize with synthetic IDs (crypto package)    │
└─────────────────────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────┐
│ 8. Run matcher pipeline (create link edges)        │
└─────────────────────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────┐
│ 9. Export to configured sinks (file/HTTP)          │
└─────────────────────────────────────────────────────┘
                        │
                        ▼
┌─────────────────────────────────────────────────────┐
│ 10. Mark checkpoint as completed, log summary       │
└─────────────────────────────────────────────────────┘
```

### Code Organization

```
internal/connectors/stripe/
  ├─ stripe.go             # Existing connector
  ├─ bulk_import.go        # NEW: Auto-pagination methods
  ├─ bulk_import_test.go   # NEW: Bulk import tests
  └─ rate_limiter.go       # NEW: Enhanced rate limiting (extract from stripe.go)

internal/backfill/
  ├─ backfill.go           # NEW: Backfill orchestrator
  ├─ checkpoint.go         # NEW: Checkpoint management
  ├─ progress.go           # NEW: Progress tracking
  └─ backfill_test.go      # NEW: Tests

main.go
  └─ runBackfillMode()     # NEW: CLI entry point for --backfill
```

---

## Acceptance Criteria

1. ✅ **Pagination**: Agent fetches ALL Stripe data regardless of volume (tested with 10k+ records)
2. ✅ **Customers**: Customer data imported with no PII exposure
3. ✅ **Progress**: Real-time progress logs during import (e.g., "Fetched 500/2500 customers")
4. ✅ **Checkpoint**: Failed import can be resumed from last checkpoint
5. ✅ **Rate Limiting**: Respects Stripe rate limits, handles 429 gracefully
6. ✅ **Synthetic IDs**: All imported data uses tenant-scoped synthetic IDs
7. ✅ **Export**: Exported data includes billing records AND link edges (matched data)
8. ✅ **Metrics**: Prometheus metrics track import progress and errors
9. ✅ **Idempotency**: Running backfill twice produces same output (deterministic synthetic IDs)
10. ✅ **Exit Clean**: Backfill mode completes and exits (doesn't run indefinitely)

---

## Test Plan

### Unit Tests
- Auto-pagination with mocked Stripe responses
- Checkpoint save/restore logic
- Rate limiter backoff behavior

### Integration Tests
1. **Small Import** (minikube + Stripe test mode):
   - Seed 50 customers, 100 subscriptions, 200 invoices
   - Run `--backfill`
   - Verify all records exported with synthetic IDs

2. **Large Import** (Stripe test mode):
   - Seed 5000 customers, 10k subscriptions, 50k invoices (via script)
   - Run `--backfill`
   - Verify pagination, rate limiting, progress tracking
   - Interrupt mid-way, verify checkpoint saved
   - Resume from checkpoint, verify completion

3. **Rate Limit Simulation**:
   - Mock 429 responses from Stripe
   - Verify exponential backoff
   - Verify recovery and completion

### Performance Tests
- Measure throughput: records/second
- Verify <10% memory growth during large imports
- Confirm rate limiter stays under Stripe limits

---

## Deliverables

1. **Code**:
   - `internal/connectors/stripe/bulk_import.go` - auto-pagination methods
   - `internal/backfill/` package - orchestrator, checkpoint, progress
   - `main.go` - `--backfill` flag and `runBackfillMode()`

2. **Configuration**:
   - Updated `config.yaml` with `backfill:` section
   - Environment variable overrides for backfill options

3. **Documentation**:
   - `/docs/backfill.md` - how to run initial import
   - Update README.md with backfill instructions
   - Add example: "Onboarding a new customer with 10k+ Stripe records"

4. **Tests**:
   - Unit tests for pagination, checkpoint, rate limiting
   - Integration test script: `scripts/test-backfill.sh`

5. **Metrics & Monitoring**:
   - Prometheus metrics for backfill progress
   - Grafana dashboard template (optional)

---

## Dependencies

- Existing Stripe connector (`internal/connectors/stripe/`)
- Existing crypto/synthetic ID system (`internal/crypto/`)
- Existing matcher pipeline (`internal/matchers/`)
- Existing exporter (`internal/exporter/`)

---

## Non-Goals (Out of Scope)

- ❌ Incremental sync (handled by webhook mode)
- ❌ Two-way sync (agent is read-only)
- ❌ Custom object import (only Stripe core objects)
- ❌ Real-time streaming during backfill (batch mode only)
- ❌ Multi-tenant backfill (one agent per tenant)

---

## Security & Privacy Considerations

1. **No PII Logging**:
   - Progress logs use counts only, no customer emails/names
   - Checkpoint file contains only Stripe IDs (opaque strings)

2. **Synthetic ID Determinism**:
   - Same Stripe ID + same tenant salt = same synthetic ID
   - Ensures idempotency across multiple backfill runs

3. **Tenant Salt Protection**:
   - Never log or export tenant salt
   - Checkpoint file does not contain salt

4. **API Key Security**:
   - Stripe API key loaded from environment only
   - Never written to checkpoint or logs

---

## Future Enhancements (Phase 2)

1. **Delta Sync**:
   - `--backfill --since=<timestamp>` to fetch only recent changes
   - Combine with webhook mode for catch-up after downtime

2. **Parallel Fetching**:
   - Fetch customers, subscriptions, invoices concurrently
   - Requires careful rate limit coordination

3. **Advanced Filtering**:
   - Filter by customer metadata, subscription status, invoice status
   - Useful for selective imports (e.g., only active subscriptions)

4. **Webhook Transition**:
   - Auto-transition from backfill mode to webhook mode
   - Detect overlap and dedup events

5. **Cloud-Triggered Backfill**:
   - Cloud sends control command to trigger backfill
   - Useful for re-sync or data correction

---

**Author:** Tomas Zezula
**Status:** Backlog
**Priority:** High (needed for customer onboarding)

