# Crypto & Models — Implementation Brief (for Claude)

> **Scope:** Implement cryptographic helpers for synthetic IDs and link proofs, plus **PII-free models** and sanitizers that convert Stripe objects into internal types. This lays the foundation for privacy-preserving matching and export. Follow `CLAUDE.md` at all times.

---

## 🧠 Context (do not change)
- Language: **Go ≥ 1.22**
- Zero-ownership: **no PII must be logged, stored, or exported**.
- Current repo structure (subset):
  ```
  linker-agent/
  ├─ internal/crypto/          # ← implement here
  │  └─ crypto.go
  ├─ internal/models/          # ← implement here
  │  └─ models.go
  ├─ internal/connectors/stripe/
  │  ├─ stripe.go              # already implemented
  │  └─ sanitize.go            # ← add here
  ├─ go.mod
  └─ ...
  ```
- Dependencies: **standard library only** for this task.

---

## 🎯 Goals

1) **Crypto helpers** (`internal/crypto/crypto.go`)
   - Deterministic HMAC for synthetic IDs using tenantSalt.
   - Stable link-proof hash over inputs for auditability.

2) **PII-free models** (`internal/models/models.go`)
   - Types used across the agent and exporter; safe to serialize without PII.

3) **Stripe sanitizers** (`internal/connectors/stripe/sanitize.go`)
   - Map **subset of safe fields** from `stripe-go` objects → internal models.
   - Absolutely no emails, names, addresses, or free-form metadata.

4) **Unit tests** proving determinism, isolation from PII, and mapping correctness.

---

## 📦 Files to create/update

1. `internal/crypto/crypto.go`
2. `internal/crypto/crypto_test.go`
3. `internal/models/models.go`
4. `internal/connectors/stripe/sanitize.go`
5. `internal/connectors/stripe/sanitize_test.go`

---

## ✅ Functional Requirements

### 1) Crypto helpers

Create `internal/crypto/crypto.go` with:

```go
package crypto

import "errors"

// HMACSHA256Hex computes HMAC-SHA256(tenantSalt, raw) and returns lowercase hex.
// The tenantSalt is provided by the tenant and MUST NOT be logged or printed.
func HMACSHA256Hex(tenantSalt []byte, raw string) (string, error)

// LinkProofHex produces a stable SHA-256 hex hash over the provided inputs,
// joined with a non-colliding separator. This is used to prove reconciliation
// inputs and recipe versions without exposing PII.
func LinkProofHex(inputs ...string) (string, error)

// (Optional) ConstantTimeEqual compares two hex strings in constant time.
func ConstantTimeEqualHex(a, b string) (bool, error)
```

**Rules:**
- Use `crypto/hmac`, `crypto/sha256`, and `crypto/subtle` where appropriate.
- Return errors for nil/empty salts or invalid hex comparisons; **do not** log secrets.
- Never include input values or salts in error strings.

---

### 2) PII-free models

Create `internal/models/models.go` with:

```go
package models

// Opaque synthetic identifiers (HMAC-derived upstream; never raw IDs).
type SynUserID string
type SynStripeID string

type SubItem struct {
    PriceID   string
    ProductID string
    Quantity  int64
}

type Subscription struct {
    ID        string      // Stripe subscription ID
    Customer  SynStripeID // Stripe customer ID (opaque)
    Status    string
    Items     []SubItem
    CreatedAt int64       // unix seconds
}

type Invoice struct {
    ID           string
    Customer     SynStripeID
    Subscription string
    Total        int64     // minor units (cents)
    Currency     string
    Status       string
    CreatedAt    int64
    Paid         bool
}
```

**Rules:**
- IDs are **opaque strings**; do not parse or derive meaning from them.
- No fields for names, emails, addresses, phone, or free-form metadata.
- These types must be safe to serialize and export as-is.

---

### 3) Stripe sanitizers

Create `internal/connectors/stripe/sanitize.go` with:

```go
package stripe

import (
    "github.com/stripe/stripe-go/v83"
    "github.com/connektn/linker-agent/internal/models"
)

// SanitizeSubscription maps a stripe.Subscription into a PII-free models.Subscription.
func SanitizeSubscription(s *stripe.Subscription) m.Subscription

// SanitizeInvoice maps a stripe.Invoice into a PII-free models.Invoice.
func SanitizeInvoice(i *stripe.Invoice) m.Invoice
```

**Mapping Guidelines:**
- **Allowed fields** only:
  - Subscription: `ID`, `Customer` (ID string), `Status`, `Created`, each item’s `Price.ID`, `Price.Product`, `Quantity`.
  - Invoice: `ID`, `Customer` (ID string), `Subscription` (ID string), `Total`, `Currency`, `Status`, `Created`, `Paid`.
- **Ignore**: `Email`, `Name`, `Addresses`, `BillingDetails`, `Shipping`, `Metadata`, `Description`, `Notes`, etc.
- For timestamps (Stripe int64): pass through as `int64` (unix seconds).
- For amounts: keep Stripe minor units (cents).

---

## 🧪 Tests

### `internal/crypto/crypto_test.go`
- **HMAC determinism:** same salt + same input → same hex.
- **HMAC salt isolation:** different salts → different hex.
- **LinkProof stability:** same inputs order → same hex; different order → different hex (document this choice).
- **Constant-time compare:** positive and negative cases.

### `internal/connectors/stripe/sanitize_test.go`
- Build minimal fake `stripe.Subscription` & `stripe.Invoice` instances and ensure:
  - Only whitelisted fields are present in the mapped model.
  - No PII fields from the source are copied.
  - Timestamps and amounts map correctly.

**Notes:** Construct Stripe objects minimally (set only required fields) to avoid coupling to unrelated behavior.

---

## 🔐 Security & Privacy Notes
- Never log or print raw identifiers, salts, or Stripe objects.
- Treat all inputs as sensitive; **error messages must be generic**.
- Sanitizers operate on **whitelists only**; if Stripe adds new fields, they are ignored by default.

---

## 🧭 Acceptance Criteria
- All new files compile and tests pass with `go test ./...`.
- Crypto functions are deterministic, safe, and do not leak secrets.
- Sanitizers return PII-free models with only approved fields.
- Code is idiomatic Go with clear GoDoc on public functions/types.

---

## 🧰 Hints
- Use a hard-to-collide separator in `LinkProofHex`, e.g., `"\x1f"` (unit separator).
- Consider preallocating buffers when joining inputs, but keep it simple.
- Keep functions short and focused for reviewability.

---

## ✅ Deliverables
- `internal/crypto/crypto.go`
- `internal/crypto/crypto_test.go`
- `internal/models/models.go`
- `internal/connectors/stripe/sanitize.go`
- `internal/connectors/stripe/sanitize_test.go`
