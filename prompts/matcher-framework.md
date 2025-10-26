# Matcher Framework — Implementation Brief (for Claude)

> **Scope:** Implement the **Matcher Framework** — the intelligence layer that links sanitized usage events with sanitized billing data (subscriptions/invoices) and emits **explainable link edges** with confidences and proofs. No PII. Deterministic first, then probabilistic, with an ensemble and thresholds defined in config.

---

## 🧠 Context (do not change)
- Language: **Go ≥ 1.22**
- Privacy: All identifiers are **synthetic**, derived by HMAC using tenantSalt. **Never** log raw IDs.
- Inputs:
  - **Usage events** (sanitized): `UsageEvent{ User SynUserID, Feature string, SKU string, At int64 }`
  - **Billing objects** (sanitized): `models.Subscription`, `models.Invoice` from Stripe sanitizer.
- Output:
  - **Edges**: `LinkEdge{ From string, To string, Kind string, Confidence float64, Proof string, Recipe string, At int64, Notes string }`
  - Where `From` and `To` are synthetic IDs (e.g., syn_user ↔ syn_sub / syn_inv / syn_price).
- Export: Edges batched through existing **Exporters** (HTTP/file), zero PII.

Repository layout additions:
```
linker-agent/
├─ internal/matchers/
│  ├─ matcher.go           # interfaces & core types
│  ├─ deterministic.go     # HMAC/ID join matcher
│  ├─ temporal.go          # time-window proximity matcher
│  ├─ sku_overlap.go       # SKU/price/product overlap matcher
│  ├─ ensemble.go          # weighted combination + thresholds
│  └─ matcher_test.go      # unit tests
├─ internal/pipeline/
│  └─ pipeline.go          # orchestration over inputs (stub here)
└─ internal/models/
   └─ (existing)
```

---

## 🎯 Goals
1) Define a **Matcher** interface and common **LinkEdge** model.
2) Implement three matchers:
   - **DeterministicIDMatcher**: exact joins on synthetic IDs (confidence = 1.0).
   - **TemporalMatcher**: correlates usage and billing by time-window proximity.
   - **SKUOverlapMatcher**: correlates usage SKUs with subscription items (price/product).
3) Implement an **Ensemble** that combines matchers via weighted scoring with a threshold.
4) Make everything **explainable** via `LinkEdge.Proof` + `Notes`.
5) Provide **configurable recipes** (weights, windows, thresholds).
6) Provide tests and small data stubs.

---

## 📦 Files to create

1. `internal/matchers/matcher.go` — core interfaces and types
2. `internal/matchers/deterministic.go`
3. `internal/matchers/temporal.go`
4. `internal/matchers/sku_overlap.go`
5. `internal/matchers/ensemble.go`
6. `internal/matchers/matcher_test.go` — unit tests
7. `internal/pipeline/pipeline.go` — simple orchestrator stub (acceptance demo)

---

## ✅ Functional Requirements

### 1) Core types & interfaces (`matcher.go`)

```go
package matchers

import "time"

// UsageEvent is sanitized and PII-free.
type UsageEvent struct {
    User       string  // SynUserID
    Feature    string  // e.g., "export_csv"
    SKU        string  // e.g., "addon_export_pro"
    At         int64   // unix seconds
}

// LinkEdge is the output: explainable, exportable, PII-free.
type LinkEdge struct {
    From       string   // source node id (e.g., syn_user or syn_sess)
    To         string   // target node id (e.g., syn_sub, syn_inv, syn_price)
    Kind       string   // "user->subscription", "user->invoice", etc.
    Confidence float64  // 0..1
    Proof      string   // crypto.LinkProofHex(...) over inputs
    Recipe     string   // recipe name/version
    At         int64    // event time or decision time
    Notes      string   // short natural-language rationale (no PII)
}

// Matcher interface
type Matcher interface {
    Name() string
    Match(usages []UsageEvent, subs []SubscriptionLite, invs []InvoiceLite) ([]LinkEdge, error)
}

// Lite billing types (sanitized; subset).
type SubscriptionLite struct {
    ID        string   // syn_sub
    Customer  string   // syn_cust
    PriceIDs  []string // syn_price
    ProductIDs []string // syn_prod
    CreatedAt int64
    Status    string
}

type InvoiceLite struct {
    ID           string // syn_inv
    Customer     string // syn_cust
    Subscription string // syn_sub
    Total        int64
    Currency     string
    Status       string
    CreatedAt    int64
    Paid         bool
}

// Configuration for the ensemble
type Recipe struct {
    Name              string
    Version           string
    Weights           map[string]float64 // per matcher name
    Threshold         float64            // accept if combined score >= threshold
    TemporalWindowSec int64              // default window for temporal matcher
    SKUOverlapMin     float64            // min Jaccard/overlap to contribute
}

// Utility
func Within(ts, center, windowSec int64) bool {
    if windowSec <= 0 { return false }
    d := ts - center
    if d < 0 { d = -d }
    return d <= windowSec
}
```

> Keep types small. Do not import Stripe types here; only use sanitized strings.

---

### 2) Deterministic matcher (`deterministic.go`)

Purpose: join on **synthetic IDs** directly where possible.

Heuristics:
- If `UsageEvent.User == sub.Customer`, then `user -> subscription` with **confidence = 1.0**.
- If an `InvoiceLite.Customer == UsageEvent.User`, link `user -> invoice` with **confidence = 1.0**.
- If event SKU equals any `sub.PriceIDs`/`ProductIDs`, boost confidence (still deterministic if exact).

```go
type DeterministicIDMatcher struct{}

func (m DeterministicIDMatcher) Name() string { return "deterministic_id" }

func (m DeterministicIDMatcher) Match(usages []UsageEvent, subs []SubscriptionLite, invs []InvoiceLite) ([]LinkEdge, error) {
    // Build in-memory indexes: by customer, by price/product.
    // For each usage, create edges to matching subs/invs (confidence 1.0).
    // Proof: LinkProofHex(user, subID or invID, "deterministic_id", feature, sku)
    // Notes: "exact id match"
    return edges, nil
}
```

> Deterministic matches should **not** be averaged; ensemble will cap at 1.0 if weight lifts above threshold.

---

### 3) Temporal matcher (`temporal.go`)

Purpose: correlate spikes in usage around billing events.

Heuristics:
- If usage time `u.At` is within `±TemporalWindowSec` around an **invoice.CreatedAt** or **subscription.CreatedAt**, contribute a score (e.g., 0.4–0.6).

Config:
- Use `Recipe.TemporalWindowSec` for the window.

```go
type TemporalMatcher struct {
    WindowSec int64
}

func (m TemporalMatcher) Name() string { return "temporal_proximity" }

func (m TemporalMatcher) Match(usages []UsageEvent, subs []SubscriptionLite, invs []InvoiceLite) ([]LinkEdge, error) {
    // For each usage event, find invoices/subs close in time.
    // Emit edges with confidence in [0,1), e.g., 0.5 base.
    // Proof: LinkProofHex(user, targetID, "temporal", strconv.FormatInt(diffSec,10))
    // Notes: "usage within 30m of invoice"
    return edges, nil
}
```

---

### 4) SKU overlap matcher (`sku_overlap.go`)

Purpose: use SKU/price/product overlap to connect usage to purchased plans/addons.

Heuristics:
- If `UsageEvent.SKU` matches/overlaps any of `sub.PriceIDs` or `ProductIDs` → high score (0.7–0.9).

```go
type SKUOverlapMatcher struct{}

func (m SKUOverlapMatcher) Name() string { return "sku_overlap" }

func (m SKUOverlapMatcher) Match(usages []UsageEvent, subs []SubscriptionLite, invs []InvoiceLite) ([]LinkEdge, error) {
    // Per usage, identify subs with matching price/product IDs.
    // Confidence can be 0.8 for exact, 0.6 for partial (if you extend logic later).
    // Proof: LinkProofHex(user, subID, "sku_overlap", sku)
    // Notes: "sku matched subscription price"
    return edges, nil
}
```

---

### 5) Ensemble (`ensemble.go`)

Combine evidence per (user,target) pair using weighted sum from the **Recipe**. Apply a **threshold** to accept.

```go
type Ensemble struct {
    Recipe Recipe
    Matchers []Matcher
}

type CandidateKey struct {
    From string // user
    To   string // sub/inv
    Kind string
}

func (e Ensemble) Combine(all []LinkEdge) []LinkEdge {
    // Group by CandidateKey.
    // Sum weights * confidences by matcher name.
    // Keep max Proof (e.g., deterministic if present) or concatenate notes.
    // If combinedScore >= Threshold → emit single LinkEdge with:
    // - Confidence = min(1.0, combinedScore)
    // - Proof = LinkProofHex(all component proofs...)
    // - Notes = joined rationale (short)
}
```

> Deterministic matches should push combined score to ≥1.0 (cap at 1.0).

---

### 6) Pipeline stub (`internal/pipeline/pipeline.go`)

Provide a tiny orchestrator function to demonstrate end-to-end:

```go
package pipeline

import (
    "context"
    "github.com/connektn/linker-agent/internal/matchers"
    "github.com/connektn/linker-agent/internal/exporter"
)

type Inputs struct {
    Usages []matchers.UsageEvent
    Subs   []matchers.SubscriptionLite
    Invs   []matchers.InvoiceLite
}

func Run(ctx context.Context, ens matchers.Ensemble, exp *exporter.Exporter, in Inputs) error {
    var all []matchers.LinkEdge
    for _, m := range ens.Matchers {
        es, err := m.Match(in.Usages, in.Subs, in.Invs)
        if err != nil { return err }
        all = append(all, es...)
    }
    combined := ens.Combine(all)
    for _, edge := range combined {
        // enqueue each edge for export
        if err := exp.Enqueue(ctx, edge); err != nil {
            return err
        }
    }
    return nil
}
```

---

## ⚙️ Configuration (extend existing YAML)

Add a new block:

```yaml
matchers:
  recipe:
    name: "default"
    version: "v1"
    weights:
      deterministic_id: 1.0
      temporal_proximity: 0.5
      sku_overlap: 0.8
    threshold: 0.8
    temporalWindowSec: 3600   # 1h
    skuOverlapMin: 0.5
```

Add a struct to `internal/config/config.go` and plumb values into `Ensemble`.

---

## 🔐 Security & Privacy
- Only synthetic IDs in edges.
- `Proof` = `crypto.LinkProofHex(...)` over opaque inputs (IDs, matcher name, recipe version), **never** feature names if sensitive — keep short labels.
- No event payloads logged. Edges are safe to export.

---

## 🧪 Tests (`matcher_test.go`)
- Build tiny fixtures:
  - 2 users, 2 subs, 2 invoices, 5 usage events.
  - One exact deterministic match; one temporal-only; one sku-only.
- Assert:
  - Deterministic edges have confidence 1.0.
  - Ensemble combines partial evidences to exceed threshold.
  - Proofs are present (non-empty) and deterministic across runs.
  - No raw (non-synthetic) IDs present in any edge.
- Optional: Bench small batches to ensure matchers are O(n log n) with indexes.

---

## 🧭 Acceptance Criteria
- Three matchers implemented with explainable edges.
- Ensemble sums weighted confidences and thresholds correctly.
- Pipeline stub can run: feed inputs → edges enqueued to exporter without errors.
- Unit tests pass (`go test ./...`).
- No PII or raw IDs anywhere in outputs or logs.

---

## ✅ Deliverables
- `internal/matchers/matcher.go`
- `internal/matchers/deterministic.go`
- `internal/matchers/temporal.go`
- `internal/matchers/sku_overlap.go`
- `internal/matchers/ensemble.go`
- `internal/matchers/matcher_test.go`
- `internal/pipeline/pipeline.go`
