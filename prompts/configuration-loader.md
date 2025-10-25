# Configuration Loader — Implementation Brief (for Claude)

> **Scope:** Implement a minimal, production-sane configuration loader for the Connektn **Linker Agent** that reads a YAML file, supports `env:` indirections for secrets, validates required fields, and exposes a typed `Config` for downstream components (e.g., Stripe connector).

This brief is self-contained and intended to be pasted into Claude Code alongside the repository. Follow the instructions precisely and keep to the privacy-first principles in `CLAUDE.md`.

---

## 🧠 Context (do not change)
- Language: **Go ≥ 1.22**
- Repo structure (subset relevant to this task):
  ```
  linker-agent/
  ├─ cmd/linker/              # entrypoint (already exists / will be added later)
  ├─ internal/config/         # ← implement here
  │  └─ config.go
  ├─ config.example.yaml      # ← provide example
  └─ go.mod                   # require yaml.v3
  ```
- **Zero-ownership**: never read or print raw secrets/PII to logs. The loader may resolve secrets but **must not** log their values.

---

## 🎯 Goal
Implement a configuration loader that:
1) Loads YAML from a path (CLI flag will be `--config`).
2) Resolves values in the form `env:NAME` using `os.Getenv("NAME")`.
3) Validates presence and correctness of required fields.
4) Returns a typed `Config` struct for use by the rest of the app.

---

## 📦 Files to create/update
1. `internal/config/config.go` — main implementation.
2. `config.example.yaml` — minimal working example end users can copy.
3. `go.mod` — ensure `gopkg.in/yaml.v3` is required (do not remove existing requirements).

---

## ✅ Functional Requirements

### 1) YAML schema (typed structs)
Implement the following structures and tags exactly:

```go
type Server struct {
    Addr string `yaml:"addr"`
}

type Privacy struct {
    Mode       string `yaml:"mode"`        // "strict" | "standard"
    TenantSalt string `yaml:"tenantSalt"`  // may be "env:NAME"
}

type StripeSource struct {
    APIKey               string `yaml:"apiKey"` // may be "env:NAME"
    Account              string `yaml:"account"`
    MaxRequestsPerSecond int    `yaml:"maxRequestsPerSecond"`
}

type Sources struct {
    Stripe *StripeSource `yaml:"stripe"`
}

type Config struct {
    Server  Server  `yaml:"server"`
    Privacy Privacy `yaml:"privacy"`
    Sources Sources `yaml:"sources"`
}
```

### 2) Loader function
Provide:
```go
func Load(path string) (Config, error)
```
- Read file → unmarshal YAML → resolve `env:` indirections → validate.
- **Do not** print secrets; return typed errors.

### 3) `env:` resolver
- Any string value beginning with `env:` should be replaced with `os.Getenv(NAME)`.
- Whitespaces around the `NAME` are trimmed.
- Empty env result is allowed at this stage; validation decides if missing is an error.

### 4) Validation rules
- `server.addr` **must** be non-empty.
- `privacy.mode` ∈ {`strict`, `standard`} (case-sensitive).
- `privacy.tenantSalt` **must** be non-empty after env resolution.
- If `sources.stripe` exists:
  - `apiKey` **must** be non-empty after env resolution.
  - `maxRequestsPerSecond` ≤ 0 → set default to **8**.
- Return `error` with clear messages; no panics.

### 5) Logging
- The loader itself should **not** log. Leave logging to the caller.
- **Never** include secrets or PII in errors.

---

## 🧪 Non-functional Requirements
- Idiomatic, small functions; keep it dependency-light.
- `go vet` and `golangci-lint` should pass (assume default settings).
- Add inline comments for tricky bits (e.g., why defaults are set).

---

## 📝 Examples

### `config.example.yaml`
```yaml
server:
  addr: ":8080"

privacy:
  mode: "strict"                 # strict | standard
  tenantSalt: "env:TENANT_SALT"  # resolved from environment

sources:
  stripe:
    apiKey: "env:STRIPE_API_KEY" # resolved from environment
    account: ""                  # optional (Stripe Connect account)
    maxRequestsPerSecond: 8
```

### Example usage (caller side)
```go
cfg, err := config.Load(*configPath)
if err != nil {
    // handle without printing secrets
    log.Fatalf("config error: %v", err)
}
// cfg is now ready to wire into components
```

---

## 🔐 Security & Privacy Notes
- Do **not** attempt to “test” env values by printing them.
- The loader must treat all string values as potentially sensitive.
- Keep error strings generic (e.g., “stripe apiKey must be set”), never echo actual values.

---

## 📦 go.mod — dependency
Ensure the loader compiles by requiring YAML:
```bash
go get gopkg.in/yaml.v3@v3.0.1
```
(Claude: update `go.mod` if missing, but do not drop existing requirements.)

---

## 🧪 Acceptance Criteria
- `Load(path)` returns a populated `Config` or a precise error.
- `env:` indirections resolve correctly (unit test recommended).
- Validation behaves as specified; defaults applied for Stripe RPS.
- No logging side effects; no secrets printed.
- The code is clean, documented, and easy to extend later.

---

## 🧰 (Optional) Unit test sketch
You may add `internal/config/config_test.go` with table tests covering:
- literal values
- env-indirections
- invalid privacy.mode
- missing tenantSalt
- stripe present with/without API key
- maxRequestsPerSecond defaulting

---

## ✅ Deliverables
- `internal/config/config.go`
- `config.example.yaml`
- (optional) `internal/config/config_test.go`

Keep the patch minimal and focused on the loader only.
