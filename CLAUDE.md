# 🤖 CLAUDE.md — Connektn Linker Agent AI Development Guidelines

This document provides **project-specific guidance for AI collaborators** (Claude, Cursor MCP, GPT-based devs, etc.) working on the Connektn Linker Agent.  
The goal is to ensure consistency, security, and privacy compliance across all code contributions.

---

## 🧩 Project Summary

**Connektn Linker Agent** is the **on-premise, open-source component** of the Connektn Zero-Ownership CDP.  
It runs inside a tenant’s infrastructure to **reconcile usage data with Stripe billing data** using **synthetic identifiers** and **privacy-preserving matchers**.

The agent **never stores or transmits PII** — only anonymized graphs and cryptographic proofs of linkage.

**Primary language:** Go  
**Packaging:** Single static binary, Docker image, Helm chart  
**Scope:** Connectors, matchers, crypto, observability, and event export.

---

## 🧠 Mission for AI Collaborators

When generating or modifying code:
1. Always **preserve the zero-ownership principle** — never introduce raw identifiers, PII fields, or plaintext logging.
2. Favor **clarity and auditability** over cleverness.  
3. Use **standard Go practices** — idiomatic, lint-clean, dependency-minimal.
4. Document security-sensitive code paths explicitly.
5. Generate code that a **human engineer can review and trust at first glance.**

---

## 🧱 Project Architecture Overview

```
linker-agent/
 ├─ cmd/linker/            # entrypoint (main.go)
 ├─ internal/connectors/   # stripe, snowflake, bigquery, postgres, posthog
 ├─ internal/matchers/     # hmac, temporal, sku_overlap, similarity
 ├─ internal/crypto/       # synthetic ID and proof hashing
 ├─ internal/otel/         # OpenTelemetry setup
 ├─ internal/config/       # YAML parsing and env overrides
 ├─ internal/http/         # healthz, metrics, readiness
 ├─ internal/exporter/     # send synthetic link graphs to Connektn Cloud
 ├─ internal/store/        # local cache (BoltDB/SQLite)
 └─ Dockerfile
```

---

## 🧩 Coding Standards

### Language and Style
- Use **Go ≥ 1.22**.
- Follow `go fmt`, `go vet`, `staticcheck`, and `golangci-lint`.
- Keep functions short and single-purpose.
- Avoid external dependencies unless security or interoperability demand them.

### Packages
- Each `internal/*` package should expose a single cohesive concept.
- No cyclic imports.
- Keep all CLI logic in `cmd/linker/main.go`.

### Comments
- All exported functions, structs, and constants **must** have GoDoc comments.
- For crypto, matching, or connector logic: include a short **“Threat model”** comment block.

---

## 🔐 Privacy and Security Guidelines

**Never:**
- Log, print, or transmit customer PII (emails, names, addresses).
- Use non-deterministic identifiers without salt derivation.
- Persist unencrypted data locally.

**Always:**
- Derive synthetic IDs using `HMAC(key=tenantSalt, message=rawID)` (SHA-256).
- Sign every match with a `linkProof` (HMAC over input hashes + recipe version).
- Use secure random (`crypto/rand`) when generating salts or IDs.
- Make external calls over HTTPS only.
- Add privacy-level unit tests for new connectors.

---

## ⚙️ Implementation Priorities

| Priority | Area | Description |
|-----------|-------|-------------|
| 🟩 P1 | `internal/connectors/stripe` | Minimal read-only Stripe connector using official Go SDK. |
| 🟩 P1 | `internal/matchers/hmac` | Deterministic ID join using synthetic hashes. |
| 🟨 P2 | `internal/matchers/temporal` | Time-window correlation (e.g., signup ↔ invoice). |
| 🟨 P2 | `internal/exporter` | Batch + retry logic for synthetic link graph uploads. |
| 🟦 P3 | `internal/matchers/sku_overlap` | Product SKU overlap analysis. |
| 🟦 P3 | `internal/matchers/similarity` | Behavioral cosine similarity (later phase). |

---

## 🧩 Configuration Principles

- Parse YAML via `internal/config`.
- Allow environment variable overrides (12-factor style).
- Default to **strict privacy mode**:
  ```yaml
  privacy:
    mode: strict
    redactUnknownFields: true
  ```
- Fail fast on misconfiguration; don’t silently continue.

---

## 📊 Observability Guidelines

- Expose Prometheus metrics via `/metrics`.
- Use OpenTelemetry for tracing (HTTP, connectors, matchers).
- No user data in metric labels.
- Example metric names:
  - `linker_matches_total`
  - `linker_confidence_avg`
  - `linker_connector_errors_total`
- Log in structured JSON via `zap` or stdlib `log/slog`.

---

## 🧰 Testing Strategy

- **Unit tests:** each matcher and connector must have ≥ 80 % coverage.
- **Integration tests:** use Dockerized Stripe mock + Postgres Testcontainer.
- **Privacy regression tests:** verify that no raw IDs or emails appear in logs.
- **Performance tests:** ensure matcher pipelines handle 10 k events/sec on 2 CPU.

---

## 🐳 Build & Release

- `make build` → static binary under `dist/`
- `make docker` → Docker image (`ghcr.io/connektn/linker`)
- Sign all releases with **cosign** and include **SBOM**.
- Dockerfile must:
  - Use `scratch` or `distroless/base` as final image.
  - Copy only the binary + minimal CA bundle.
  - Run as non-root user.

---

## 🧱 Example Folder Conventions

```
internal/connectors/stripe/
 ├─ client.go       # wraps Stripe API
 ├─ fetch.go        # retrieval functions
 ├─ model.go        # typed objects (sanitized)
 └─ stripe_test.go

internal/matchers/hmac/
 ├─ matcher.go      # core logic
 ├─ proof.go        # linkProof generation
 └─ hmac_test.go
```

---

## 🔎 AI Coding Checklist

Before submitting AI-generated code:
1. ✅ Lint passes with `golangci-lint run`.
2. ✅ Unit tests pass (`make test`).
3. ✅ No external dependency introduces telemetry or analytics.
4. ✅ No secret, key, or PII literal is hard-coded.
5. ✅ Logs contain only synthetic IDs or anonymized metrics.
6. ✅ Functions have clear comments, including security rationale.

---

## 🧠 Knowledge Resources

When unsure, refer to:
- [Stripe API Reference](https://stripe.com/docs/api)
- [OpenTelemetry Go SDK](https://pkg.go.dev/go.opentelemetry.io/otel)
- [Go Security Best Practices](https://go.dev/security/)
- [OWASP Secure Coding Practices](https://owasp.org/www-project-secure-coding-practices/)

---

> “The Linker Agent is not a data collector. It’s a trust enforcer.”
