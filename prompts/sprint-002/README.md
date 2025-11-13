# Connektn Phase 2 – Kubernetes & Operational Resilience

## Overview
Phase 2 focuses on **scaling, observability, and operational control** of the Connektn platform.  
After Phase 1 enabled noninvasive feature usage reporting, this phase ensures the system can run reliably in **Kubernetes**, handle large-scale workloads, and expose **tenant-visible management** for Linker Agents.

---

## Stories

| Story                                                                       | Purpose                                                   | Key Deliverable                                               |
|-----------------------------------------------------------------------------|-----------------------------------------------------------|---------------------------------------------------------------|
| [Kubernetes Deployment & Helm Charts](story-01-kubernetes-deployment.md)    | Package all components for cluster-based deployment       | Helm charts for Linker Agent, Gateway Adapter, and full stack |
| [Tenant Agent Management & Heartbeats](story-02-tenant-agent-management.md) | Expose on-prem agent health & controls in Cloud dashboard | Heartbeat system + tenant-visible control APIs                |
| [Monitoring & Observability](story-03-monitoring-observability.md)          | Unified metrics, logs, and tracing across all components  | Prometheus metrics, Grafana dashboards, OpenTelemetry traces  |

---

## Technical Objectives
- **Helm-first deployment:** simple installation, secure defaults, rolling upgrades.
- **Scalable ingestion:** multi-agent and multi-tenant setups supported by HPA.
- **Operational visibility:** Prometheus metrics, Grafana dashboards, and alerts.
- **Tenant transparency:** agents visible and controllable in the Cloud UI.
- **Resilience:** durable queues, graceful restarts, replay guarantees.

---

## Deployment Profiles
| Profile  | Description                                             | Components                                  |
|----------|---------------------------------------------------------|---------------------------------------------|
| **Lite** | Single-node or edge deployment using embedded WAL queue | Linker Agent + PVC storage                  |
| **HA**   | Multi-node cluster using Kafka/Redpanda or RabbitMQ     | Agents + consumer groups + Prometheus stack |

---

## Metrics & Observability Highlights
- **Metrics:** queue depth, DLQ size, retries, p95 latency, heartbeat health.
- **Logging:** structured JSON logs, correlation IDs, Loki/ELK compatible.
- **Tracing:** OpenTelemetry support from Gateway → Agent → Cloud.
- **Alerts:** missing heartbeat, queue backlog, high retry rate.

---

## Tenant Controls (from Phase 2.2)
- View agent status (mode, uptime, queue depth, DLQ size).
- Execute signed control commands (restart, stop, upgrade, switch mode).
- Detect unhealthy agents and raise alerts automatically.

---

## Success Criteria
1. Full Connektn stack deploys via Helm with single command.
2. Cloud dashboard shows real-time agent health.
3. End-to-end monitoring in Prometheus/Grafana.
4. Alerts fire correctly under simulated failures.
5. mTLS security validated across all components.

---

## Next Steps (Phase 3 Preview)
Phase 3 will build on this foundation to introduce **multi-region data replication**, **advanced analytics**, and **self-serve onboarding flows** for SaaS customers.

---

**Author:** Tomas Zezula  
**Phase:** 2  
**Codename:** *Kubernetes & Operational Resilience*
**Status:** In Progress
