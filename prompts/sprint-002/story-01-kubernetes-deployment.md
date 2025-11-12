# Story 1 – Kubernetes Deployment & Helm Charts

## Before you start
Read and follow all rules in `CLAUDE.md`.  
Work within the Connektn monorepo; align chart values with agent configuration from Phase 1.

---

## Goal
Package the **Linker Agent**, **Gateway Adapter**, and **SDK emitter sidecars** into fully deployable Helm charts for Kubernetes.  
Enable both Lite (embedded queue) and HA (Kafka/Rabbit) profiles through Helm values.

---

## Scope
### Helm Chart Structure
- `charts/connektn-agent/` – Linker Agent + ConfigMap templates.
- `charts/connektn-gateway/` – Envoy/NGINX Gateway Adapter deployments.
- Optional `charts/connektn-stack/` – umbrella chart (agent + gateway + optional Kafka/Rabbit).

### Features
- Configurable values for:
  - replicas, resources, storage class, queue type (embedded/kafka/rabbit)
  - env vars for security and rate limits
  - mTLS secrets and certificates
- Support `kubectl port-forward` and `Ingress` exposure for testing.
- Include liveness and readiness probes.
- Enable graceful shutdown hooks (flush and WAL replay).

### Profiles
| Profile  | Description                                                                               |
|----------|-------------------------------------------------------------------------------------------|
| **Lite** | Single-agent pod with embedded WAL queue (PVC attached).                                  |
| **HA**   | Multi-agent deployment using Kafka/Rabbit backend, consumer groups for shipper/validator. |

---

## Acceptance Criteria
1. One-command install: `helm install connektn ./charts/connektn-stack/` works for both profiles.
2. Rolling updates complete with zero data loss.
3. WAL persists and replays after crash.
4. Helm values override tested for resource tuning, queue, and security options.
5. CI pipeline lints charts and runs `helm template` validation.
6. Example manifests for minikube and cloud (EKS/GKE/AKS) clusters.

---

## Deliverables
- Helm charts with `values.yaml` templates for Lite and HA.
- Example K8s manifests and `README.md` for each chart.
- CI tests under `.github/workflows/helm.yml` for lint + deploy + teardown.

---

**Author:** Tomas Zezula  
**Status:** Backlog
