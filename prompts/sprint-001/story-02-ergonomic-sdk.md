# Story 2 – Ergonomic SDK

## Before you start
Read and follow all rules in `CLAUDE.md`.  
Use the existing Connektn repository layout.  
Maintain cross-language consistency and zero-PII guarantees.

---

## Goal
Implement an **Ergonomic SDK** in Java/Kotlin, Node.js, and Python that allows customers to explicitly report feature usage from their business code.  
All SDKs must share a unified event schema and delivery semantics.

---

## Context
The SDK is the precision layer complementing the no-code Gateway Adapter.  
It should be simple, resilient, and identical in semantics across languages.

---

## Scope
### Core Features
- Initialize with optional parameters (service, version, env, endpoint).
- Provide APIs:
  - `featureStart(id, opts?) -> span`
  - `feature(id, fn|opts)` (auto times the function)
  - `emit(id, opts)` (instantaneous event)
  - `setContext({ service, version, env, attributes })`
- Automatically batch and send events to Linker Agent at `unix:///var/run/connektn/agent.sock /v1/feature-events`.
- Support disk spooling when Agent unavailable; flush on recovery.
- Implement drop policy (`drop_oldest`) when spool cap exceeded.
- Implement local backpressure (respect `Retry-After` header).
- Sampling: configurable via `CONNEKTN_SAMPLE` (default 1.0).
- No blocking behavior on the request path.

### Shared Event Schema
```
feature_id, feature_name, category, service, version, env, start_ts, duration_ms, outcome, attributes, sample_rate
```
- Validate field types and block any PII-like keys (regex: `email|user|token|cookie`).
- Autogenerate `event_id = sha256(org_id || feature_id || start_ts)`.

### Language-Specific Tasks
- **Java/Kotlin:** ThreadLocal context; coroutine bridge; auto flush on shutdown hook.
- **Node.js:** Async batching via worker thread; Express/Fastify helper.
- **Python:** Asyncio worker; decorator helpers for WSGI/ASGI frameworks.

---

## Acceptance Criteria
1. All SDKs emit identical JSON structures given the same event.
2. Each SDK passes golden-event conformance tests.
3. Local buffering, batching, and retry confirmed via integration tests.
4. Performance: <1ms added latency per event at 95th percentile.
5. Configurable via env vars only; zero required code secrets.
6. Safe shutdown flushes pending batches.
7. When Agent is down, spooling persists; on recovery, events are delivered in order.

---

## Test Plan
- Unit: serialization, validation, sampling, PII filtering.
- Integration: end-to-end with Linker Agent mock (simulate 5xx, backoff, recovery).
- Stress: 10k events/sec sustained, no drops beyond configured policy.
- Golden JSON comparison between SDKs.
- Security: ensure only local endpoints accepted; inspect for accidental PII.

---

## Deliverables
- SDKs under `sdks/java/`, `sdks/node/`, `sdks/python/`.
- Shared conformance tests under `sdks/tests/`.
- Example apps demonstrating SDK usage.
- README for each SDK + unified schema doc under `/docs/sdk/`.

---

**Author:** Tomas Zezula  
**Status:** Backlog
