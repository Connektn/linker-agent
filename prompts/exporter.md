# Exporter — Implementation Brief (for Claude)

> **Scope:** Implement a reliable, privacy-preserving exporter that batches and transmits **synthetic link data** (no PII) to the Connektn Cloud ingestion API via HTTPS.  
> The exporter ensures delivery integrity, retry safety, and tenant isolation.  
> Follow `CLAUDE.md` for coding and security standards.

---

## 🧠 Context
- Language: **Go ≥ 1.22**
- Runs inside the **Linker Agent**.
- Input: synthetic models (`internal/models`) from matchers or the Stripe connector.
- Output: HTTPS POST requests to the Connektn Cloud ingest endpoint.
- Dependencies:  
  - Standard library only (`net/http`, `encoding/json`, `sync`, `time`, `context`).  
  - No external HTTP clients, no telemetry libraries.

---

## 📦 Files to create

```
linker-agent/
└─ internal/exporter/
   ├─ exporter.go
   ├─ queue.go
   ├─ exporter_test.go
```

---

## 🎯 Goals

1. Implement a lightweight, resilient exporter with:
   - **Buffered queue** for outgoing payloads
   - **Retry with backoff**
   - **Graceful shutdown**
   - **Idempotency control**
   - **Zero PII** transmission

2. Provide a clean interface so higher layers (matchers, connectors) can simply call:
   ```go
   exp.Enqueue(ctx, payload)
   ```

3. Support both synchronous flush (for testing) and background batching (for production).

---

## ✅ Functional Requirements

### 1) Interface

```go
package exporter

type Exporter struct {
    endpoint   string
    tenantKey  string
    client     *http.Client
    queue      chan []byte
    wg         sync.WaitGroup
}

type Options struct {
    Endpoint   string        // e.g. https://api.connektn.io/ingest
    TenantKey  string        // authentication key (not secret, tenant-scoped)
    BatchSize  int           // default 50
    FlushEvery time.Duration // default 5s
    MaxRetries int           // default 3
}
```

Constructor:

```go
func New(opts Options) (*Exporter, error)
```

Core methods:

```go
func (e *Exporter) Enqueue(ctx context.Context, payload any) error
func (e *Exporter) Start(ctx context.Context)
func (e *Exporter) Shutdown(ctx context.Context) error
```

---

### 2) Enqueue
- Accepts any serializable struct (models from matchers, invoices, etc.).  
- Serializes to JSON (`encoding/json.Marshal`).  
- Adds to a buffered channel (default capacity = 5 × BatchSize).  
- Returns immediately (non-blocking if capacity available).  
- If full, drop oldest entry and record a metric counter in a later iteration (for now, comment a `TODO`).

---

### 3) Background Worker (`Start`)
- Goroutine loop selecting on:
  - Channel receives → accumulate up to `BatchSize`
  - Timer → flush every `FlushEvery`
- On flush:
  - Build JSON array of payloads.
  - POST to `opts.Endpoint`.
  - Headers:
    ```
    Authorization: Bearer <TenantKey>
    Content-Type: application/json
    Idempotency-Key: <uuidv4>
    ```
  - Handle response codes:
    - `2xx` → success
    - `>=500` → retry with exponential backoff (2s, 4s, 8s, …)
    - `>=400` → discard batch (bad request).
- Backoff implemented via `time.Sleep`, capped by `MaxRetries`.

---

### 4) Shutdown
- Gracefully stop background loop.
- Flush any remaining queued payloads synchronously.
- Wait for active requests (`sync.WaitGroup`).

---

### 5) Error Handling
- Wrap all errors (`fmt.Errorf("export flush: %w", err)`).
- Do not log payloads or sensitive fields.
- Log only summary events like “flushed N events” or “retrying batch”.

---

## 🧪 Non-functional Requirements
- Fully deterministic, testable logic.
- Unit tests must simulate network failures, retries, and graceful shutdown.
- No dependencies beyond the Go stdlib.
- Follow `CLAUDE.md` privacy and observability rules.
- Future integration with `/metrics` counters should be easy.

---

## 🧪 Tests (`internal/exporter/exporter_test.go`)
- **Enqueue test:** verify channel buffering and overflow handling.
- **Flush success:** mock HTTP 200, confirm payload posted and cleared.
- **Flush retry:** mock HTTP 500 → retry logic applied.
- **Flush discard:** mock HTTP 400 → batch dropped without panic.
- **Shutdown:** enqueue items, call shutdown, confirm final flush runs once.

---

## 🔐 Security & Privacy Notes
- Never include event data or payloads in logs.
- Do not retry indefinitely — enforce max retry count.
- Tenant key is not secret but must still be transmitted over HTTPS.
- Use `http.Transport` with sane timeouts:
  ```go
  &http.Transport{
      IdleConnTimeout: 30 * time.Second,
      MaxIdleConns:    10,
  }
  ```

---

## 🧭 Acceptance Criteria
- Exporter correctly batches and flushes payloads via HTTPS.
- Retries on transient (5xx) errors, discards bad requests.
- Gracefully shuts down without data loss.
- No PII or sensitive data appears in logs or errors.
- Tests cover enqueue, flush, retry, and shutdown paths.

---

## ✅ Deliverables
- `internal/exporter/exporter.go`
- `internal/exporter/queue.go`
- `internal/exporter/exporter_test.go`
