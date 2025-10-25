# Stripe Connector — Implementation Brief (for Claude)

> **Scope:** Implement a minimal, privacy-safe **Stripe connector** for the Connektn **Linker Agent**. The connector must use read-only Stripe APIs, support Connect accounts, avoid PII entirely, and expose a small typed interface that higher layers can call. Target stability and clarity over features.

This brief is self-contained. Follow `CLAUDE.md` for architecture, security, and coding standards.

---

## 🧠 Context (do not change)
- Language: **Go ≥ 1.22**
- Use official **`github.com/stripe/stripe-go/v83`**.
- Repo structure (subset relevant to this task):
  ```
  linker-agent/
  ├─ internal/connectors/stripe/
  │  ├─ stripe.go          # ← implement here
  │  └─ stripe_test.go     # ← optional unit tests
  ├─ internal/config/
  │  └─ config.go          # already implemented by previous task
  ├─ cmd/linker/
  │  └─ main.go            # will wire a debug endpoint
  └─ go.mod
  ```
- Configuration is already loaded via `internal/config`:
  ```yaml
  sources:
    stripe:
      apiKey: env:STRIPE_API_KEY
      account: ""                   # optional Connect account ID
      maxRequestsPerSecond: 8
  ```

---

## 🎯 Goal
Provide an exported **Client** with a small, privacy-safe surface:

```go
package stripec

type Client struct {
    // internal fields; no PII
}

func New(apiKey, account string, maxRPS int) (*Client, error)

// SmokeCheck performs a tiny read to verify credentials (no PII in output).
func (c *Client) SmokeCheck(ctx context.Context) (count int, err error)

// ListSubscriptions lists subscriptions for an optional customer, returning a sanitized view.
func (c *Client) ListSubscriptions(ctx context.Context, customerID string, limit int64) ([]Subscription, error)

// ListInvoices lists invoices for an optional customer/subscription, returning a sanitized view.
func (c *Client) ListInvoices(ctx context.Context, customerID, subscriptionID string, limit int64) ([]Invoice, error)
```

Where sanitized models contain **no PII**:

```go
type Subscription struct {
    ID        string
    Customer  string         // Stripe ID only
    Status    string
    Items     []SubItem
    CreatedAt int64          // unix seconds
}

type SubItem struct {
    PriceID   string
    ProductID string
    Quantity  int64
}

type Invoice struct {
    ID            string
    Customer      string        // Stripe ID only
    Subscription  string        // Stripe ID only
    Total         int64         // amount in cents
    Currency      string
    Status        string
    CreatedAt     int64         // unix seconds
    Paid          bool
}
```

> **Never** return or log fields like email, name, address, phone, or free-form text.

---

## ✅ Functional Requirements

### 1) Client initialization
- Implement `New(apiKey, account string, maxRPS int) (*Client, error)`:
  - Validate non-empty `apiKey`.
  - Initialize `stripe-go` client.
  - Store optional `account` for Connect requests (`SetStripeAccount` on params).
  - Prepare a basic internal rate limiter (see below).

### 2) Rate limiting (minimal, in-memory)
- Implement a simple token bucket or leaky-bucket limiter:
  - Default capacity = `max(1, maxRPS)` tokens per second.
  - Non-blocking try-acquire with short sleep fallback (≤ 50ms) to smooth bursts.
  - Apply to each list call to be a good API citizen.
- Keep it simple and dependency-free (use `time.Ticker` / `time.After`).

### 3) SmokeCheck
- List up to **5 customers** to validate credentials.
- Return the **count** only; do not log customer attributes.
- Handle Connect account via `SetStripeAccount` on params.

### 4) ListSubscriptions
- Parameters: `customerID` (optional), `limit` (if 0 → default 10).
- Use `stripe.Subscriptions.List` with pagination.
- Map to sanitized `Subscription`:
  - Use only IDs, status, timestamps, and item price/product IDs, quantities.
  - Do **not** copy embedded customer/email objects, metadata containing PII, or address fields.

### 5) ListInvoices
- Parameters: `customerID` (optional), `subscriptionID` (optional), `limit` (if 0 → default 10).
- Use `stripe.Invoices.List` with pagination.
- Map to sanitized `Invoice` (IDs, totals, currency, timestamps, statuses only).

### 6) Error handling
- Wrap errors with context (`fmt.Errorf("stripe list invoices: %w", err)`).
- Do not leak request IDs or secrets in error strings.
- Return empty slices on success; never `nil` plus `nil`.

### 7) Logging
- The connector **must not** log. Leave logging to callers.
- Add GoDoc and brief inline comments for security/privacy rationale.

---

## 🧪 Non-functional Requirements
- Idiomatic Go; small, focused functions.
- No external deps beyond `stripe-go`.
- Deterministic behavior suitable for unit tests.
- Support cancellation via `context.Context` on all public methods.

---

## 🧰 Implementation Hints

- Use `https://github.com/stripe/stripe-go/tree/v83.0.2/client` and call `api.Subscriptions.List`, `api.Invoices.List`, etc.
- Apply `params.SetStripeAccount(c.account)` if `account != ""`.
- Map **only** the allowed fields into your sanitized types.
- For timestamps, use fields like `s.Created` or `i.Created` and convert to `int64` (unix seconds).
- For amounts, Stripe uses **minor units** (e.g., cents). Keep them as `int64`.

---

## 🧪 Acceptance Criteria

- `New` fails if `apiKey` is empty; succeeds otherwise.
- `SmokeCheck` returns `0..5` without PII exposure.
- `ListSubscriptions` / `ListInvoices` return sanitized slices with IDs and numeric fields only.
- Rate limiter present and applied (unit-testable via small RPS and timeouts).
- No logs, no PII anywhere in code or tests.
- Public functions and structs have clear GoDoc.

---

## 🧪 Optional Tests (`internal/connectors/stripe/stripe_test.go`)

- Constructor validation (empty apiKey → error).
- SmokeCheck with a mocked client (use small interfaces, or construct behind feature flag).
- Mapping tests: build minimal fake `*stripe.Subscription` / `*stripe.Invoice` values and ensure sanitized output excludes PII.
- Rate limiter sanity: with `maxRPS = 1`, two acquire calls take ≥ ~1s in total (tolerate small jitter).

---

## 🔒 Security & Privacy Notes
- Do not read or log fields like `Customer.Email`, `BillingDetails`, `Shipping`, `Metadata`.
- If Stripe adds new fields in nested objects, ignore by default. Only map the whitelist above.
- Keep all identifiers as **opaque strings**; do not attempt to parse or generate customer IDs.

---

## 🧭 Next Step (caller wiring)
After this task, the `cmd/linker/main.go` will wire a `/debug/stripe-check` HTTP handler calling `SmokeCheck` to verify credentials during manual testing.

---

## ✅ Deliverables
- `internal/connectors/stripe/stripe.go`
- (optional) `internal/connectors/stripe/stripe_test.go`
