# Story 3 – Monitoring & Observability

## Before you start
Read and follow all rules in `CLAUDE.md`.  
Ensure consistency with existing Linker Agent metrics and follow Prometheus exposition format.

---

## Goal
Implement comprehensive **monitoring and observability** for Connektn's on-prem and cloud components using Prometheus, Grafana, and structured logging.

---

## Scope
### Metrics
- Expose Prometheus metrics from Linker Agent, Gateway Adapter, and Cloud ingest:
  - `connektn_events_total` (labels: source, outcome)
  - `connektn_events_dropped_total`
  - `connektn_queue_depth`
  - `connektn_dlq_size`
  - `connektn_shipper_retries_total`
  - `connektn_latency_p95_seconds`
- Add `/metrics` endpoint (or sidecar exporter) to all services.
- Add Grafana dashboards:
  - Agent health (queue depth, DLQ, retries)
  - Cloud ingest throughput and error rate
  - Top features by volume and error
  - System latency breakdown (ingest → shipper → cloud)

### Logs
- Structured JSON logging (Loki/ELK compatible): timestamp, level, source, org_id, event_id, message.
- Correlation IDs across gateway ↔ agent ↔ cloud pipelines.
- Centralized log configuration via `log.yaml`.

### Tracing
- Integrate OpenTelemetry exporters (OTLP/gRPC) for agent and cloud services.
- Trace IDs propagate via context headers or gRPC metadata.
- Trace visualization in Grafana Tempo or Jaeger.

### Alerts
- Prometheus alert rules:
  - Queue depth > threshold
  - DLQ size > threshold
  - Ship retry rate > threshold
  - Missing heartbeat > 2 intervals
- Preconfigured alertmanager templates.

---

## Acceptance Criteria
1. All metrics available in Prometheus within 10s scrape interval.
2. Grafana dashboards render without manual setup.
3. Traces connect gateway → agent → cloud events.
4. Alert rules trigger under simulated failures.
5. Logs include correlation IDs and conform to JSON schema.
6. <1% overhead on event throughput.

---

## Test Plan
- Integration: spin up minikube + Prometheus + Grafana + Agent.
- Verify metrics correctness and scrape times.
- Simulate backpressure to test alert rules.
- Inject test traces and confirm visibility in Tempo/Jaeger.
- Validate JSON log schema.

---

## Deliverables
- Prometheus metrics endpoints in all components.
- `grafana/dashboards/` with importable JSON templates.
- `prometheus/alerts.yaml` with default alert rules.
- Structured logging schema documentation under `/docs/logging.md`.
- Helm chart integration for automatic PrometheusServiceMonitor registration.

---

**Author:** Tomas Zezula  
**Status:** Backlog
