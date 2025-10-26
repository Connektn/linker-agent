# Wire Matcher into Pipeline — Implementation Brief (for Claude)

> **Scope:** Plug the Matcher Framework into a minimal Pipeline that collects sanitized inputs, runs matchers, and exports **LinkEdge** results via the existing **Exporter**. The Exporter must persist edges to a **file** and/or send them to a **dedicated HTTP endpoint** based on config. No PII anywhere.

---

## 🧠 Context
- Language: **Go ≥ 1.22**
- Components already implemented:
  - Config loader (`internal/config`)
  - Stripe connector + sanitizers (`internal/connectors/stripe`)
  - Crypto + synthetic IDs (`internal/crypto`, `internal/models`)
  - Exporter (dual sink: HTTP + File) (`internal/exporter`)
  - Matcher Framework (`internal/matchers`) + `Ensemble`
- Missing piece: **Pipeline wiring** (feed inputs → run matchers → export edges).

---

## 🎯 Goal
1) Provide a **Pipeline** that:
   - Pulls sanitized billing data (subs, invoices).
   - Accepts sanitized usage events (stub source for now).
   - Runs the **Ensemble** of matchers.
   - Streams resulting `LinkEdge` objects to the **Exporter**.

2) Add a **CLI command** (or debug HTTP endpoint) that executes the pipeline end-to-end so we can see file/HTTP exports happening without AI help.

3) Exported output **must** reflect matcher relationships:
   - If file sink enabled → append JSONL to `reports/link_edges.jsonl` (one edge per line).
   - If HTTP sink enabled → POST to a dedicated edge endpoint, e.g. `/ingest/edges`.

---

## 📦 Files to create/update

1. `internal/pipeline/pipeline.go` — orchestrator
2. `cmd/linker/main.go` — add `--run-pipeline` flag and simple runner
3. `internal/exporter/exporter.go` — ensure it can write generic payloads (edges) to file/HTTP (reuse existing dual-sink logic)
4. `internal/pipeline/pipeline_test.go` — minimal happy-path test

> Do **not** change matcher internals. Only wire and plumb config & flows.

---

## ✅ Functional Requirements

### 1) Pipeline orchestrator
Create `internal/pipeline/pipeline.go`:

```go
package pipeline

import (
    "context"
    "fmt"

    "github.com/connektn/linker-agent/internal/exporter"
    "github.com/connektn/linker-agent/internal/matchers"
)

type Inputs struct {
    Usages []matchers.UsageEvent
    Subs   []matchers.SubscriptionLite
    Invs   []matchers.InvoiceLite
}

// Run executes all matchers in the ensemble, combines edges, and enqueues them to the exporter.
func Run(ctx context.Context, ens matchers.Ensemble, exp *exporter.Exporter, in Inputs) error {
    var all []matchers.LinkEdge
    for _, m := range ens.Matchers {
        es, err := m.Match(in.Usages, in.Subs, in.Invs)
        if err != nil { return fmt.Errorf("matcher %s: %w", m.Name(), err) }
        all = append(all, es...)
    }
    final := ens.Combine(all)
    for _, e := range final {
        if err := exp.Enqueue(ctx, e); err != nil {
            return fmt.Errorf("export enqueue: %w", err)
        }
    }
    return nil
}
```

### 2) Usage event stub (for now)
Add a tiny helper in `cmd/linker/main.go` to create a few **synthetic** usage events (PII-free) so we can run the pipeline without a real SDK:

```go
func seedUsage() []matchers.UsageEvent {
    now := time.Now().Unix()
    return []matchers.UsageEvent{
        {User: "syn_cust_demoA", Feature: "export_csv", SKU: "syn_price_demo1", At: now - 600},
        {User: "syn_cust_demoB", Feature: "api_calls",  SKU: "syn_price_demo2", At: now - 120},
    }
}
```

> Later this will be replaced by your real local usage source (SDK/agent).

### 3) Stripe → Lite mapping
Add a small mapping utility (either in `cmd/linker/main.go` or `internal/connectors/stripe/sanitize.go`) that converts sanitized Stripe objects to `matchers.SubscriptionLite` and `matchers.InvoiceLite` (IDs already synthetic; copy only allowed fields).

### 4) CLI flag to run pipeline
In `cmd/linker/main.go`, add:
- `--run-pipeline` (bool) to execute once and exit.
- When set:
  1. Load config incl. `matchers.recipe` and `export` (mode: `http|file|both`).
  2. Construct **Exporter** with the chosen sinks:
     - File path for edges: `reports/link_edges.jsonl` by default if file sink.
     - Endpoint for edges: `<export.endpoint>/ingest/edges` if HTTP sink.
  3. Build **Ensemble** with configured weights/window/threshold.
  4. Fetch Stripe data: a page of subscriptions & invoices (via your connector), sanitize → lite.
  5. Build a small slice of usage events (`seedUsage()`).
  6. Call `pipeline.Run(ctx, ens, exporter, Inputs{...})`.
  7. Print: `✅ Pipeline run complete (edges exported)`.

### 5) Exporter behavior (dual sink)
- Reuse the existing exporter (no behavior change), but ensure that writing a **single edge** per JSON line to the file sink is supported when `payload` is a `LinkEdge` or a slice thereof.
- For HTTP sink, send arrays of edges to `/ingest/edges` with headers already supported (`Authorization`, `Content-Type`, `Idempotency-Key`).

---

## ⚙️ Config (extend YAML if needed)

Ensure we have:

```yaml
export:
  mode: "file"           # http | file | both
  endpoint: "https://api.example.com"  # used if mode has http
  filePath: "reports/link_edges.jsonl" # used if mode has file

matchers:
  recipe:
    name: "default"
    version: "v1"
    weights:
      deterministic_id: 1.0
      temporal_proximity: 0.5
      sku_overlap: 0.8
    threshold: 0.8
    temporalWindowSec: 3600
    skuOverlapMin: 0.5
```

Validation:
- If `export.mode` includes `"file"`, ensure `export.filePath` exists or can be created.
- If `export.mode` includes `"http"`, ensure `export.endpoint` is non-empty.

---

## 🧪 Tests
Create `internal/pipeline/pipeline_test.go`:
- Build fake `UsageEvent`s + `SubscriptionLite` / `InvoiceLite` with synthetic IDs.
- Use a **test exporter** that captures `Enqueue` calls in-memory (or a temp file path).
- Assert:
  - `Run()` emits at least one edge when deterministic or SKU overlap exists.
  - No panics, errors nil.
  - Output edges contain only synthetic IDs (prefix `syn_`).

> Keep tests small and deterministic.

---

## 🔐 Privacy & Security
- The pipeline must **never** log payloads or raw identifiers.
- All IDs are synthetic at this stage; exporter must not serialize secrets.
- Keep errors generic (no payload echo).

---

## 🧭 Acceptance Criteria
- `go run ./cmd/linker --config config.yaml --run-pipeline` executes matchers and exports edges using the configured sinks.
- When file sink is enabled, `reports/link_edges.jsonl` is created with one JSON edge per line.
- When HTTP sink is enabled, the POST to `<endpoint>/ingest/edges` succeeds (2xx) or retries per exporter rules.
- Unit test for pipeline passes.

---

## ✅ Deliverables
- `internal/pipeline/pipeline.go`
- `cmd/linker/main.go` (flag + wiring)
- (optional) mapping helper for Lite structs
- `internal/pipeline/pipeline_test.go`
