# Connektn Phase 1 – Noninvasive Feature Reporting

## Overview
This phase introduces a **holistic, zero‑touch feature usage reporting** system.  
The goal is to enable Connektn customers to start capturing feature insights across any stack (Java, .NET, Node.js, Python, etc.) **without changing application code**.

Phase 1 consists of three coordinated stories:

| Story                                                                              | Purpose                                                   | Key Deliverable                                                        |
|------------------------------------------------------------------------------------|-----------------------------------------------------------|------------------------------------------------------------------------|
| [Gateway Adapter](story-01-gateway-adapter.md)                                     | Framework‑agnostic ingress observer for HTTP/gRPC traffic | Envoy WASM + NGINX module emitting `FeatureStart`/`FeatureStop` events |
| [Ergonomic SDK](story-02-ergonomic-sdk.md)                                         | Lightweight explicit tracking API for business code       | Unified SDKs for Java/Kotlin, Node.js, Python                          |
| [Linker Agent Queues & Backpressure](story-03-linker-agent-queues-backpressure.md) | Reliable ingestion and durable queueing on‑prem           | Embedded + Kafka/Rabbit profiles with DLQ + replay tools               |

All three components share the same **event schema** and **communication protocol** with the Linker Agent.

---

## Technical Highlights
- **Zero‑code start** via Gateway Adapter; **precision opt‑in** via SDK.
- **Local durability** through Linker Agent’s write‑ahead log or external queue.
- **Backpressure‑aware batching** to protect cloud ingest endpoints.
- **PII‑free guarantees** enforced in all components.
- **Unified telemetry** for drop counts, queue lag, retries, DLQ size.

---

## Deployment Profiles
| Profile  | Description                                     | Components                                           |
|----------|-------------------------------------------------|------------------------------------------------------|
| **Lite** | Single host / Docker / no external dependencies | Embedded WAL queue in Linker Agent                   |
| **HA**   | Kubernetes / high throughput                    | Kafka/Redpanda or RabbitMQ queue, scalable consumers |

---

## Next Steps
1. Implement each story according to `CLAUDE.md` guardrails.  
2. Validate event conformance across adapters and SDKs.  
3. Integrate the Linker Agent’s metrics into the Cloud Dashboard.  
4. Prepare documentation for customer onboarding and deployment recipes.

---

## Success Criteria
- Customers can connect Stripe + deploy Linker Agent + enable Gateway Adapter within 30 minutes.  
- End‑to‑end events survive agent downtime with zero data loss.  
- Same schema and semantics across SDKs and adapters.  
- <1% overhead on application throughput.

---

**Author:** Tomas Zezula  
**Phase:** 1  
**Codename:** *Noninvasive Feature Reporting*
**Status:** Backlog

