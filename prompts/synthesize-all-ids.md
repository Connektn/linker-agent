# Synthesizing All IDs — Implementation Brief (for Claude)

> **Scope:** Enforce tenant-scoped synthetic identifiers across **all** Stripe entities exported by the Connektn Linker Agent. No native Stripe IDs (e.g., `cus_`, `sub_`, `in_`, `ch_`, `re_`) may leave the tenant boundary. This aligns with the zero-ownership CDP principle and prevents indirect re-identification via non-customer IDs.

---

## 🧠 Rationale (do not change)
- A subscription (`sub_...`) has exactly one customer; an invoice (`in_...`) links to a subscription or customer. If we leak `sub_` or `in_` IDs, customer re-identification becomes feasible, defeating anonymized customer IDs.
- Therefore, **all exported identifiers must be synthetic**, derived with a tenant-specific secret salt via HMAC-SHA256.

---

## 🎯 Goal
1) Introduce a single helper to derive synthetic IDs with stable prefixes.  
2) Update all sanitizers and models so **every ID field** is synthetic before export.  
3) Add tests to ensure no raw Stripe IDs appear in exported payloads.

---

## 📦 Files to create/update
- `internal/crypto/id.go` (new) — helper for synthetic IDs
- `internal/connectors/stripe/sanitize.go` — map to synthetic IDs (update)
- `internal/models/models.go` — ensure fields store synthetic IDs (confirm/update types)
- `internal/exporter/...` — (no behavior change, but add a guard test)
- `internal/connectors/stripe/sanitize_test.go` — tests for synthetic ID mapping
- `internal/exporter/exporter_test.go` — negative test: panic or error if raw IDs detected (optional light check)

---

## ✅ Functional Requirements

### 1) Synthetic ID helper
Create `internal/crypto/id.go`:

```go
package crypto

import (
    "fmt"
)

// SyntheticID returns a prefixed, lowercase-hex HMAC over the raw ID.
// Example: SyntheticID(salt, "cus_123", "syn_cust") => "syn_cust_92f6ab3e10"
// The output length should be short but collision-resistant for our scale: first 16 hex chars is fine.
func SyntheticID(tenantSalt []byte, raw, prefix string) (string, error) {
    h, err := HMACSHA256Hex(tenantSalt, raw)
    if err != nil {
        return "", err
    }
    if len(h) > 16 {
        h = h[:16]
    }
    return fmt.Sprintf("%s_%s", prefix, h), nil
}
```

> Use existing `HMACSHA256Hex` from `internal/crypto/crypto.go`. Keep prefixes readable and consistent.

### 2) Prefix policy (consistent naming)
| Entity        | Native example | Synthetic prefix |
|---------------|----------------|------------------|
| Customer      | `cus_...`      | `syn_cust`       |
| Subscription  | `sub_...`      | `syn_sub`        |
| Invoice       | `in_...`       | `syn_inv`        |
| Charge        | `ch_...`       | `syn_ch`         |
| Refund        | `re_...`       | `syn_ref`        |
| Price         | `price_...`    | `syn_price`      |
| Product       | `prod_...`     | `syn_prod`       |

> Only include the entities you actually export. Others can be added progressively.

### 3) Sanitizers must synthesize **every** ID
Update `internal/connectors/stripe/sanitize.go` to accept `tenantSalt []byte` and synthesize IDs:

```go
func SanitizeSubscription(salt []byte, s *stripe.Subscription) (m.Subscription, error) {
    id, err := crypto.SyntheticID(salt, s.ID, "syn_sub")
    if err != nil { return m.Subscription{}, err }
    cust, err := crypto.SyntheticID(salt, s.Customer.ID, "syn_cust")
    if err != nil { return m.Subscription{}, err }

    items := make([]m.SubItem, 0, len(s.Items.Data))
    for _, it := range s.Items.Data {
        var priceID, prodID string
        if it.Price != nil {
            if it.Price.ID != "" {
                priceID, _ = crypto.SyntheticID(salt, it.Price.ID, "syn_price")
            }
            if it.Price.Product != nil {
                if pid, ok := it.Price.Product.(*string); ok && pid != nil {
                    prodID, _ = crypto.SyntheticID(salt, *pid, "syn_prod")
                }
            }
        }
        items = append(items, m.SubItem{
            PriceID:   priceID,
            ProductID: prodID,
            Quantity:  it.Quantity,
        })
    }
    return m.Subscription{
        ID:        id,
        Customer:  m.SynStripeID(cust),
        Status:    string(s.Status),
        Items:     items,
        CreatedAt: int64(s.Created),
    }, nil
}
```

Do the same for invoices (and charges/refunds if you sanitize them here):

```go
func SanitizeInvoice(salt []byte, i *stripe.Invoice) (m.Invoice, error) {
    id, err := crypto.SyntheticID(salt, i.ID, "syn_inv")
    if err != nil { return m.Invoice{}, err }
    cust := ""
    if i.Customer != nil {
        if cs, ok := i.Customer.(*stripe.Customer); ok && cs != nil {
            cust, err = crypto.SyntheticID(salt, cs.ID, "syn_cust")
            if err != nil { return m.Invoice{}, err }
        } else if cid, ok := i.Customer.(*string); ok && cid != nil {
            cust, err = crypto.SyntheticID(salt, *cid, "syn_cust")
            if err != nil { return m.Invoice{}, err }
        }
    }
    sub := ""
    if i.Subscription != nil {
        if ss, ok := i.Subscription.(*stripe.Subscription); ok && ss != nil {
            sub, err = crypto.SyntheticID(salt, ss.ID, "syn_sub")
            if err != nil { return m.Invoice{}, err }
        } else if sid, ok := i.Subscription.(*string); ok && sid != nil {
            sub, err = crypto.SyntheticID(salt, *sid, "syn_sub")
            if err != nil { return m.Invoice{}, err }
        }
    }

    return m.Invoice{
        ID:           id,
        Customer:     m.SynStripeID(cust),
        Subscription: sub,
        Total:        i.Total,
        Currency:     string(i.Currency),
        Status:       string(i.Status),
        CreatedAt:    int64(i.Created),
        Paid:         i.Paid,
    }, nil
}
```

> **Important:** Handle `Customer` and `Subscription` union types (object vs string). Do not copy any PII fields.

### 4) Models: confirm synthetic types
Ensure `internal/models/models.go` continues to use **synthetic** identifiers for all IDs:

```go
type SynID string // generic opaque type for future use

type Subscription struct {
    ID        string      // synthetic sub id
    Customer  SynStripeID // synthetic customer id
    Status    string
    Items     []SubItem
    CreatedAt int64
}

type Invoice struct {
    ID           string      // synthetic invoice id
    Customer     SynStripeID // synthetic customer id
    Subscription string      // synthetic sub id
    Total        int64
    Currency     string
    Status       string
    CreatedAt    int64
    Paid         bool
}
```

If any fields still store native IDs, replace them with synthetic strings.

### 5) Call site changes
Wherever `SanitizeSubscription` / `SanitizeInvoice` are called, pass `tenantSalt` from loaded config:
```go
sub, _ := SanitizeSubscription([]byte(cfg.Privacy.TenantSalt), stripeSub)
```

---

## 🧪 Tests

### `internal/connectors/stripe/sanitize_test.go`
- Build minimal `stripe.Subscription` and `stripe.Invoice` with known IDs.
- With a fixed salt (e.g., `[]byte("test_salt")`), assert:
  - `ID`, `Customer`, `Subscription`, `Items[].PriceID`, `Items[].ProductID` all start with expected `syn_*` prefixes and **do not** equal the source IDs.
  - Stability: repeated calls return the same synthetic values.
- Negative: when `Customer` or `Subscription` is a string vs object reference, mapping still works.

### Optional exporter guard
- Add a simple check or test that exported payloads do **not** match regex `^(cus_|sub_|in_|ch_|re_|price_|prod_)` in any ID field.

---

## 🔐 Privacy & Security
- Never log raw IDs or salts.
- Synthetic IDs must be deterministic per tenant but unguessable across tenants.
- Limit synthetic hex length to 16 chars to reduce accidental reidentification via long hashes in logs.

---

## 🧭 Acceptance Criteria
- No native Stripe IDs appear in any exported payload.
- All entity IDs in models are synthetic with correct prefixes.
- Tests pass with `go test ./...`.
- Existing public interfaces remain stable (backward-compatible sanitizers now return synthetic IDs).

---

## ✅ Deliverables
- `internal/crypto/id.go`
- Updated `internal/connectors/stripe/sanitize.go`
- Updated call sites (pass salt)
- Tests in `internal/connectors/stripe/sanitize_test.go`
