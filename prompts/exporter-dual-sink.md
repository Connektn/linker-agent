# Exporter — Dual Sink Support (HTTP + File)

Task: Extend the Connektn Linker Agent exporter so that it can write outputs to
both an HTTP endpoint *and/or* a local file, as defined by configuration.

---

### 🧠 Context
The current implementation supports only an HTTP endpoint for export.
We now want to support an optional *file sink* as well — to make testing and
offline auditing easier.

Follow the architecture, privacy, and style rules from `CLAUDE.md`.
Use only the Go standard library.

---

### 🎯 Goal
Add configuration and logic so that the exporter can:
- Send payloads to an HTTP endpoint (existing behavior).
- Optionally also append the same payloads to a local file (e.g. `reports/exporter_output.jsonl`).
- Either or both sinks may be enabled via config.

---

### ⚙️ Configuration Schema Update
Extend the YAML schema and struct in `internal/config/config.go`:

```yaml
export:
  mode: "http,file,both"     # optional, default "http"
  endpoint: "https://api.connektn.io/ingest"
  filePath: "reports/exporter_output.jsonl"
```
Struct addition:
```go
type Export struct {
    Mode     string `yaml:"mode"`      // "http" | "file" | "both"
    Endpoint string `yaml:"endpoint"`
    FilePath string `yaml:"filePath"`
}
```
Validation:
* Default mode → "http".
* If mode includes "file", ensure filePath is non-empty.
* If mode includes "http", ensure endpoint is non-empty.

### 🧩 Implementation Steps

1. Update `internal/exporter/exporter.go`:
* Accept an `ExportMode` enum internally.
* Keep existing HTTP logic untouched.
* Add a new `writeToFile(payload []byte)` helper:

```go
func (e *Exporter) writeToFile(data []byte) error {
    f, err := os.OpenFile(e.filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
    if err != nil { return fmt.Errorf("open file sink: %w", err) }
    defer f.Close()
    _, err = f.Write(append(data, '\n'))
    return err
}
```
* In flushBatch(), branch according to mode:
```go
if e.mode == "file" || e.mode == "both" {
    _ = e.writeToFile(batchBytes)
}
if e.mode == "http" || e.mode == "both" {
    _ = e.postToEndpoint(batchBytes)
}
```
* Errors from one sink should not block the other.

2. Update constructor `New(opts Options)` to accept file mode and validate.
3. Add a small field to `Exporter` struct:

```go
mode     string
filePath string
```

4. Add unit tests:
* Mode file: only writes to file, no HTTP calls.
* Mode http: only sends HTTP.
* Mode both: does both.
* Invalid config → proper error.

### 🔐 Privacy & Security Rules
* File sink must never include secrets, headers, or env variables.
* Each payload should be one JSON line (append-only file).
* File write errors must be logged as warnings, not fatal.
* Ensure concurrent writes are protected by a mutex.

### ✅ Acceptance Criteria
* Config-driven export mode (http, file, both).
* Payloads appear in file when mode includes "file".
* HTTP sending unchanged and working as before.
* Graceful degradation if one sink fails.
* Tests pass with go test ./internal/exporter/....

### 📦 Deliverables
* Updated `internal/config/config.go`
* Updated `internal/exporter/exporter.go`
* Updated `internal/exporter/exporter_test.go`
