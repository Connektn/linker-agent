# Story 1 – Gateway Adapter

## Before you start
Read and follow all rules in `CLAUDE.md`.  
Use the existing Connektn repository layout.  
Keep the implementation PII-free, observable, and testable in isolation.

---

## Goal
Implement a **Gateway Adapter** that can observe feature usage without code changes in customer apps.  
The adapter must emit normalized `FeatureStart` / `FeatureStop` events to the local Linker Agent over a secure local channel.

---

## Context
Most customer apps already run behind a reverse proxy (NGINX / Envoy).  
We leverage that to capture request metadata and translate it into Connektn events.  
Adapters for HTTP and gRPC traffic must share the same emitter logic used later by the SDK.

---

## Scope
- Build `connektn-gateway-adapter` with:
  - **Envoy WASM filter** (Rust or C++) and
  - **NGINX Lua/OpenResty module** as reference implementation.
- Each must:
  - Extract: `method`, `route_template`, `status_code`, `latency_ms`, `host`, `service`, `version`, `env`.
  - Apply optional `mapping.yaml` to convert routes → feature IDs/names.
  - Serialize to the shared event schema.
  - Batch (100–500 events / 1 s) and send to `unix:///var/run/connektn/agent.sock /v1/feature-events`.
  - Support backpressure signals (`429`, `Retry-After` headers).
  - Drop oldest when local buffer full (configurable cap).
- Implement a small CLI (`gateway-adapter validate`) to test mappings and simulate traffic.

---

## Acceptance Criteria
1. Adapter emits valid event JSON confirmed by Linker Agent mock.
2. Batching and backpressure observed in integration tests.
3. `mapping.yaml` correctly resolves to human-friendly feature names.
4. No sensitive fields (headers, cookies, query params) included.
5. <1% CPU overhead at 1k RPS baseline.
6. Telemetry (Prometheus counters): total events, batched, dropped, retry count.

---

## Test Plan
- Unit: route parsing, mapping, batching, serialization.
- Integration: send events to Linker Agent mock under load (simulate 1k RPS, 50% errors).
- Failure tests: Agent unavailable, slow Agent, buffer overflow.
- Security: verify adapter refuses remote connections (local only).
- Performance: measure latency overhead (<2ms p95).

---

## Deliverables
- Source under `adapters/gateway/`
- Docker example: `examples/gateway-envoy/` with sample config.
- README explaining deployment, configuration, and mapping format.

---

**Author:** Tomas Zezula  
**Status:** Backlog
