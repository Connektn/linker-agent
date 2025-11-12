# Story 3 – Linker Agent Queues & Backpressure

## Before you start
Read and follow all rules in `CLAUDE.md`.  
Ensure full compatibility with both Gateway Adapter and SDK emitters.  
Prioritize durability, fault tolerance, and PII-free event handling.

---

## Goal
Implement durable, scalable, and backpressure-aware event ingestion within the Linker Agent.  
Support both **Profile A (Lite)** using embedded queue and **Profile B (HA)** using Kafka/Redpanda or RabbitMQ.

---

## Architecture Overview
**Flow:**
```
Emitters (Gateway / SDK)
   → Local HTTP/UDS Ingest API
      → Validator → Enricher → Queue (embedded or external)
         → Shipper → Cloud ingest
```

---

## Scope
### Core Functions
- **Ingest API:** local-only listener (UDS or 127.0.0.1:8325).
  - Auth via install key.
  - Validate schema, sampling, and size limits.
  - Respond with `429` + `Retry-After` when backpressured.

- **Queue Options:**
  - `embedded` (default): append-only WAL in RocksDB or SQLite WAL.
  - `kafka` or `rabbit` for HA deployments.
  - Configurable retention (hours, bytes) and DLQ paths.

- **Pipelines:** validator → enricher → shipper workers.
  - Validator: enforce schema, strip PII.
  - Enricher: attach org_id, service, env, version.
  - Shipper: batch & send to Cloud ingest endpoint.

- **Batching & Backpressure:**
  - Batch size 5–50 KB or 1s interval.
  - Token bucket per org (`burst`, `refill_per_sec`).
  - When Cloud returns 429 or 5xx, exponential backoff + jitter.
  - Advertise available capacity to emitters via response headers.

- **DLQ Handling:**
  - Invalid or undeliverable messages → DLQ with reason code.
  - CLI tool: `connektn-agent dlq reprocess --since ...`.

- **Resilience:**
  - Disk spool when Agent temporarily unavailable.
  - Safe restarts: WAL replay and resume from last offset.
  - Horizontal scaling (Profile B): partition by `org_id` or `(org_id,service)`.

- **Security:**
  - mTLS between Agent and Cloud.
  - Local-only ingest; apps never carry org secrets.
  - Optional payload encryption before queue write.

---

## Acceptance Criteria
1. Ingest API handles 10k events/sec sustained with <1% loss.
2. Embedded queue survives restart without data loss.
3. Kafka/Rabbit profile passes integration tests with backpressure and DLQ enabled.
4. Proper backoff & retry on Cloud unavailability.
5. CLI DLQ reprocessing tool functional.
6. Metrics exposed: queue depth, dropped events, retry count, DLQ size.
7. Agent returns `429 Retry-After` when overloaded.
8. E2E replay verified after simulated downtime.

---

## Test Plan
- Unit: validation, enrichment, token-bucket logic.
- Integration:
  - End-to-end with SDK/Gateway emitter mock.
  - Failure injection: Agent restart, Cloud 5xx, disk full.
  - Kafka + Rabbit backends under load.
- Performance: measure throughput and flush latency.
- Resilience: simulate 5min downtime, confirm replay correctness.

---

## Deliverables
- Linker Agent implementation under `agent/core/`.
- Config templates for embedded + Kafka + Rabbit.
- CLI tool for DLQ reprocessing and queue inspection.
- Documentation under `/docs/agent/queues.md` describing both profiles.

---

**Author:** Tomas Zezula  
**Status:** Backlog
