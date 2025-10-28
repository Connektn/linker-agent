# Per-Stream Export Configuration — Implementation Brief (for Claude)

> **Scope:** Refactor the Exporter config and usage so different payload *streams* (e.g., `edges`, `billing`, `usage`) can be routed to distinct HTTP endpoints and/or file paths. Maintain backward compatibility with the existing single-sink config. No PII. Stdlib only.

---

## 🧠 Context
- Language: **Go ≥ 1.22**
- Existing pieces:
  - Config loader (`internal/config`)
  - Exporter with dual sinks (HTTP + File) (`internal/exporter`)
  - Pipeline + Matchers producing `LinkEdge` results (`internal/pipeline`, `internal/matchers`)
- Goal: Keep `matchers` pure (no I/O). All delivery decisions live under **`export`** config.

---

## 🎯 Goals
1) Introduce **per-stream routes/paths** under a single `export` block:
   - HTTP base URL + per-stream routes
   - File base directory or per-stream file paths (JSONL)
2) Update Exporter API to accept a **stream key**.
3) Ensure **pipeline** exports `LinkEdge`s to the `edges` stream.
4) Provide **backward compatibility** with current fields:
   - `export.mode`, `export.endpoint`, `export.filePath`
   - If present, map them to `http.baseUrl` and `file.paths.edges`.

---

## 🧱 Config Shape (YAML)

### New preferred structure
```yaml
export:
  mode: "both"  # http | file | both
  http:
    baseUrl: "https://api.connektn.io"
    routes:
      edges: "/ingest/edges"
      billing: "/ingest/billing"
      usage: "/ingest/usage"
    headers:
      authorizationEnv: "CONNEKTN_TENANT_KEY"  # optional env name for Bearer token
    maxRetries: 3
    batchSize: 50
    flushEvery: 5s

  file:
    paths:
      edges: "reports/link_edges.jsonl"
      billing: "reports/billing.jsonl"
      usage: "reports/usage.jsonl"
```

### Back-compat mapping
If **legacy** fields exist:
```yaml
export:
  mode: "file"
  endpoint: "https://api.connektn.io/ingest"
  filePath: "reports/exporter_output.jsonl"
```
Map as:
- `http.baseUrl = export.endpoint` (if it looks like a full URL) and `http.routes.edges = "/ingest/edges"` (default).
- `file.paths.edges = export.filePath`.
- Keep defaults for `maxRetries=3`, `batchSize=50`, `flushEvery=5s`.

---

## 🧩 Code Changes

### 1) Config structs (`internal/config/config.go`)
Add:
```go
type ExportHTTP struct {
    BaseURL    string            `yaml:"baseUrl"`
    Routes     map[string]string `yaml:"routes"`   // e.g. "edges": "/ingest/edges"
    Headers    struct {
        AuthorizationEnv string `yaml:"authorizationEnv"` // env var name providing bearer token
    } `yaml:"headers"`
    MaxRetries int           `yaml:"maxRetries"`
    BatchSize  int           `yaml:"batchSize"`
    FlushEvery time.Duration `yaml:"flushEvery"`
}

type ExportFile struct {
    Paths map[string]string `yaml:"paths"` // e.g. "edges": "reports/link_edges.jsonl"
}

type Export struct {
    Mode     string     `yaml:"mode"` // http|file|both
    Endpoint string     `yaml:"endpoint"` // legacy
    FilePath string     `yaml:"filePath"` // legacy
    HTTP     ExportHTTP `yaml:"http"`
    File     ExportFile `yaml:"file"`
}
```

**Validation & migration:**
- Default `Mode="http"` if empty.
- If legacy `Endpoint` is set and `HTTP.BaseURL` empty → set `BaseURL=Endpoint` and ensure `Routes["edges"]` default `"/ingest/edges"`.
- If legacy `FilePath` is set and `File.Paths["edges"]` empty → set it.
- Ensure when `Mode` includes `"file"`, at least `File.Paths["edges"]` is resolvable.
- Ensure when `Mode` includes `"http"`, `HTTP.BaseURL` is not empty.

### 2) Exporter API (`internal/exporter/exporter.go`)
Add a **stream parameter**:

```go
// Stream identifies a logical export route/file. Example: "edges", "billing", "usage".
type Stream string

const (
    StreamEdges  Stream = "edges"
    StreamBilling Stream = "billing"
    StreamUsage   Stream = "usage"
)

// Enqueue adds a payload to the stream-specific queue.
func (e *Exporter) Enqueue(ctx context.Context, stream Stream, payload any) error
```

Internal changes:
- Maintain **per-stream buffers/queues** or tag items with `stream` and route at flush time.
- For **file** mode: append JSON (one per line) to `cfg.File.Paths[stream]`.
- For **http** mode: POST batched JSON array to `cfg.HTTP.BaseURL + cfg.HTTP.Routes[stream]`.
- If a route/path for a given `stream` is missing:
  - Return a clear error: `fmt.Errorf("export route undefined for stream %q", stream)`.

Keep existing batching/backoff/idempotency behavior.

### 3) Pipeline usage (`internal/pipeline/pipeline.go`)
- Change calls to `exp.Enqueue(ctx, exporter.StreamEdges, edge)` for matcher outputs.
- No other logic changes.

### 4) CLI/Runner minimal change (`cmd/linker/main.go`)
- No new flags required; rely on config.
- Print selected sinks and resolved `edges` destinations on startup for clarity (without secrets).

---

## 🧪 Tests

1) **Config migration test** (`internal/config/config_test.go`):
   - Legacy config → verify mapping to `HTTP.BaseURL` and `File.Paths["edges"]`.

2) **Exporter routing test** (`internal/exporter/exporter_test.go`):
   - Given `mode="file"` + `file.paths.edges`, enqueue 2 edges → file contains 2 JSON lines.
   - Given `mode="http"` + `http.baseUrl` + `routes.edges`, mock server captures POST body (array of edges).
   - Given `mode="both"`, both sinks receive data.
   - Missing route/path for stream → returns error.

3) **Pipeline smoke test** (`internal/pipeline/pipeline_test.go`):
   - Use temp file path for `edges`; run pipeline with a deterministic match → file has at least 1 line.

---

## 🔐 Privacy & Security
- Do not include secrets in logs. `AuthorizationEnv` must read token from env and only set the **Authorization header**.
- Validate that no raw Stripe IDs are serialized (rely on existing sanitizers/tests).

---

## 🧭 Acceptance Criteria
- Backward-compatible: existing configs still export edges to the previous file or endpoint.
- New per-stream config works for `edges` (and is easily extensible).
- Pipeline exports matcher `LinkEdge`s to `edges` stream; file and/or HTTP receive data depending on `mode`.
- All new tests pass.

---

## ✅ Deliverables
- Updated `internal/config/config.go` (+ tests)
- Updated `internal/exporter/exporter.go` (+ tests)
- Updated `internal/pipeline/pipeline.go` (stream param)
- Optional: startup log line summarizing active streams & sinks
