# Stripe Webhook Integration

**Scope:** Add a secure Stripe webhook receiver to the Linker Agent that verifies signatures, fetches canonical objects, sanitizes, runs the Matcher Ensemble, and exports resulting LinkEdge records via the existing Exporter (file/http streams). The webhook turns one-off backfills into a live pipeline. No PII at any point.

## 🧠 Context
- Language: Go ≥ 1.22
- Existing components:
  - Config loader (internal/config)
  - Stripe connector + sanitizers (internal/connectors/stripe)
  - Crypto + synthetic IDs (internal/crypto, internal/models)
  - Matcher Framework + Ensemble (internal/matchers)
  - Exporter (per-stream, dual sink file/http) (internal/exporter)
  - Pipeline orchestrator (internal/pipeline)
- Goal: Add `/webhooks/stripe` HTTP handler + worker that produces edges for relevant events.

## ⚙️ Config Additions (internal/config/config.go)

YAML:
```yaml
stripe:
  apiKey: "env:STRIPE_API_KEY"
  webhook:
    enabled: true
    path: "/webhooks/stripe"
    signingSecret: "env:STRIPE_WEBHOOK_SECRET"
    # Optional hardening
    allowedIPRanges: []         # e.g., ["3.18.12.63/32", "3.130.192.231/32"]
    maxSkew: 300s               # tolerance for Stripe-Timestamp
    retry:
      maxAttempts: 5
      baseBackoff: 2s
      maxBackoff: 30s

export:
  mode: "both"
  http:
    baseUrl: "https://api.connektn.io"
    routes:
      edges: "/ingest/edges"
  file:
    paths:
      edges: "reports/link_edges.jsonl"
```

Validation:
- If `webhook.enabled`: `signingSecret` must resolve from env; `path` must start with `/`.
- If `allowedIPRanges` non-empty: parse and store CIDRs; non-fatal if omitted.

## 🧩 HTTP Handler

Create package: `internal/ingest/stripewebhook`

Files:
- `handler.go` — HTTP handler + verification
- `verify.go` — signature + IP allowlist checks
- `worker.go` — object fetch + sanitize + match + export
- `handler_test.go` — unit tests
- `worker_test.go` — unit tests with fake Stripe client

### Handler responsibilities
- POST {path} only; reject others with 405.
- Read body (cap at e.g. 1MB), parse headers:
  - Stripe-Signature: verify timestamp + signature (HMAC-SHA256 over {timestamp}.{payload}) against signingSecret.
  - If allowedIPRanges configured: check RemoteAddr against CIDRs (best-effort).
- Parse minimal fields: id (event id), type, created, data.object.id (if present).
- **Idempotency**: in-memory LRU + optional disk set under var/export.queue.dir (or simple bolt/badger optional). If seen before → 200 OK, no work.
- Create a small `Job{ EventID, Type, ObjectID, Created }` and hand off to a worker (non-blocking). If queue accepted → return 200 immediately.
> Return 2xx only after job is persisted (or at least accepted by an in-memory queue with best-effort disk spill).

## 🔄 Worker (fetch → sanitize → match → export)
`worker.go`:
- Given `Job`, fetch canonical object(s) from Stripe using our connector (do NOT use webhook payload as truth).
  - For `invoice.*` → retrieve invoice (expand `subscription`, `customer` where safe).
  - For `customer.subscription.*` → retrieve subscription (expand items/prices/products).
  - For `charge.*` and `refund.*` (optional at start) → retrieve charge/refund.
- Convert to sanitized lite representations:
  - matchers.InvoiceLite
  - matchers.SubscriptionLite
- Build `Inputs{ Usages: (optional stub now), Subs, Invs }` and run the **Ensemble** via `pipeline.Run`.
  - For MVP, it’s ok if usage events are not present; deterministic/sku edges may still be produced.
- For each returned LinkEdge, export via:
```go
exporter.Enqueue(ctx, exporter.StreamEdges, edge)
```
- Implement retry with backoff if Stripe fetch fails (rate limits, temporary errors).
- Log only high-level events; never log payload or PII/raw IDs.

Supported events (MVP):
- `invoice.created`, `invoice.finalized`, `invoice.payment_succeeded`, `invoice.payment_failed`
- `customer.subscription.created`, `customer.subscription.updated`, `customer.subscription.deleted`
- (Optional) `charge.succeeded`, `charge.refunded`

Unknown types: ignore with 200 OK (to avoid retries), but record a metric counter.

## 🧪 Tests
`handler_test.go`:
- Signature verification success/failure:
  - Correct secret → 200
  - Wrong secret → 400
  - Old timestamp beyond `maxSkew` → 400
- IP allowlist (if configured): allowed vs denied.
- Idempotency: same `event.id` twice → first 200, second 200 (no duplicate job).

`worker_test.go`:
- Fake Stripe client returning minimal objects (invoice/subscription).
- Ensure worker fetches, sanitizes to synthetic IDs, calls pipeline, and enqueues edges.
- Simulate Stripe 429/5xx → retry backoff respected (bounded).
- Ensure no raw IDs in exported edges; all must have `syn_*` prefixes.

Integration test (optional):
- With `stripe listen --forward-to localhost:8080/webhooks/stripe`, trigger `stripe trigger` `invoice.payment_succeeded` and assert at least one edge is written to the file sink.

## 🔐 Security & Privacy
- Webhook signature verification is mandatory. Refuse if header missing/invalid.
- Do not log request bodies or headers.
- Store only `event.id`, `type`, `created` for idempotency/metrics.
- All object identifiers are synthetic before leaving the agent.
- If DLQ/disk persistence is implemented, encrypt-at-rest is optional but recommended for later.

## 📈 Metrics & Health
Add counters (package-level or simple global recorder):
- `webhook_received_total`
- `webhook_verified_total`
- `webhook_dup_dropped_total`
- `webhook_enqueued_total`
- `webhook_errors_total`

Expose /healthz returning 200.

## 🧭 CLI & README
- `README.md`: add “Live mode with Stripe webhooks” section.
- Local dev instructions:
```bash
export STRIPE_API_KEY=sk_test_...
export STRIPE_WEBHOOK_SECRET=whsec_...
stripe listen --forward-to localhost:8080/webhooks/stripe
stripe trigger invoice.payment_succeeded
```

## 🧪 Acceptance Criteria
- Endpoint POST `/webhooks/stripe` exists and verifies signatures.
- Supported Stripe events result in **canonical fetch → sanitize → match → export**.
- Duplicate webhook deliveries are safely ignored (idempotency).
- At least one unit test per major behavior passes; optional integration test docs provided.
- No PII or raw Stripe IDs leave the agent in any logs or exports.
