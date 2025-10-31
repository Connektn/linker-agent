# Implement dual ID modes for the Linker Agent

## Goal
- Support two privacy modes controlled by config:
    - strict: emit only synthetic IDs (HMAC salt), never leak raw platform IDs
    - passthrough: emit raw platform IDs (e.g., Stripe cus_/sub_/in_), but still no PII/PHI anywhere

## Config
Add:
```yaml
  privacy:
    idMode: "strict"        # "strict" | "passthrough"
    tenantSalt: "env:TENANT_SALT"   # required in strict
    allowPassthroughExports: false  # default; must be true to allow http export of raw IDs to non-Connektn endpoints
```

## ID mapping
Create interface:
```go
type IDMapper interface { CustomerID, SubscriptionID, InvoiceID, PriceID, ProductID, Mode() }
```
Implement:
```go
SyntheticMapper (HMAC with tenantSalt, producing "syn_<short>")
```
and `PassthroughMapper` (returns the raw IDs as-is).

Construct mapper in config init; fail fast if `strict` and `tenantSalt` missing.

## Models
- Add a Meta { schema, id_mode } to LinkEdge and any snapshot types serialized by exporter.
    - schema: "connektn.edge.v1"
    - id_mode: mapper.Mode()
- Update Proof calculation to include mapper.Mode() to prevent cross-mode replay.

## Exporter guardrails
- On startup, if idMode == "passthrough" and export.mode includes "http":
    - If allowPassthroughExports == false and http.baseUrl NOT in {https://api.connektn.dev, https://api.connektn.io} → return a clear error.
    - Otherwise log a single WARN banner "passthrough mode: exporting raw platform IDs (no PII)".
- File sink: always allowed. Add same WARN banner when idMode == "passthrough".

## Logging
- Do NOT log any IDs in either mode. Use opaque job IDs for traceability.
- Log id_mode once at startup at INFO.

## Tests
- Add golden tests for both modes:
    1) strict: edges JSON contains no /^cus_|^sub_|^in_|^price_|^prod_/; all IDs start with "syn_"; meta.id_mode == "strict".
    2) passthrough: at least one ID matches Stripe regex; meta.id_mode == "passthrough".
    3) proof differs across modes for same underlying objects.
    4) exporter guardrail: passthrough + http + disallowed baseUrl + allowPassthroughExports=false → error.
- Update existing matcher tests to run with both mappers.

## Docs
- README: add "Privacy modes" with a table of strict vs passthrough behaviors and the startup guardrail.
- Note: passthrough mode still strips PII/PHI (emails/names) and keeps zero-PII logs.

## Acceptance
- Agent runs in either mode based on config or --id-mode override.
- Edges include meta.schema=v2 and meta.id_mode.
- Strict mode never emits raw IDs; passthrough preserves platform IDs; both modes pass existing unit tests.
