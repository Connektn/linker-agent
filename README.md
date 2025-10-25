# 🧩 Connektn Linker Agent

> **Privacy-safe data reconciliation engine for Stripe-based SaaS businesses.**  
> Runs inside your infrastructure. Produces verified, anonymized customer graphs without ever exposing PII.

---

## 🔍 Overview

The **Connektn Linker Agent** is the core on-prem component of the [Connektn Zero-Ownership CDP](https://connektn.dev).  
It runs inside your environment (Docker or Kubernetes) and performs **local, privacy-safe matching** between:

- **Billing data** from Stripe, and  
- **Feature-usage / analytics data** from your warehouse, product logs, or analytics platform.

Instead of collecting raw data, the Linker Agent generates **synthetic identifiers and cryptographic proofs** of reconciliation, then sends only anonymized link graphs to the Connektn Cloud.

> “Your data never leaves your infrastructure — only the math does.”

---

## 🏗️ Architecture

```
tenant-infra
│
├─ Stripe (read-only key)
│     └── customers / subscriptions / invoices
│
├─ Warehouse / analytics source
│     └── feature usage, sessions, events
│
├─ 🔒 Connektn Linker Agent (this service)
│     ├── internal/connectors/
│     │     ├─ stripe/
│     │     ├─ snowflake/
│     │     ├─ bigquery/
│     │     └─ postgres/
│     ├── internal/matchers/
│     │     ├─ hmac/
│     │     ├─ temporal/
│     │     ├─ sku_overlap/
│     │     └─ similarity/
│     ├── internal/crypto/
│     ├── internal/otel/
│     └── cmd/linker/
│
└─ 🌐 Connektn Cloud (ingest API)
      └── receives only synthetic links + proofs
```

---

## ⚙️ Features

- 🔐 **Zero-PII:** all hashing/encryption done locally; raw data never leaves your environment.  
- 🧠 **Deterministic & probabilistic matchers:** HMAC joins, temporal proximity, SKU overlap, behavioral similarity.  
- 🧾 **Proof-of-Truth:** every match emits a cryptographic proof for later verification.  
- ⚙️ **Pluggable connectors:** Stripe, Snowflake, BigQuery, Redshift, Postgres, S3/GCS, PostHog exports.  
- 📊 **OpenTelemetry metrics:** health, throughput, confidence distributions.  
- 🐳 **Deploy anywhere:** single Go binary, Docker image, or Helm chart.  
- 🧩 **YAML-based recipes:** define matching logic declaratively, no code changes needed.  
- 🔏 **Signed releases + SBOM:** verified binaries for enterprise adoption.

---

## 🚀 Quick Start

### 1. Run with Docker

```bash
docker run -d   -e CONNEKTN_TENANT_KEY=pk_live_xxx   -e STRIPE_API_KEY=rk_readonly_xxx   -e TENANT_SALT_SECRET_ARN=arn:aws:secretsmanager:region:acct:secret:tenantSalt   -v ./recipes.yaml:/etc/connektn/recipes.yaml:ro   ghcr.io/connektn/linker:latest
```

### 2. Example `recipes.yaml`

```yaml
sources:
  stripe:
    apiKey: ${STRIPE_API_KEY}
  warehouse:
    dsn: ${WAREHOUSE_DSN}

privacy:
  mode: strict
  tenantSalt: ${TENANT_SALT_SECRET_ARN}

matchers:
  - name: stripe_customer_id
    method: hmac
    fields: ["stripe.customer.id"]
    weight: 0.6
  - name: temporal_proximity
    method: temporal
    fields: ["invoice.created_at", "usage.first_seen"]
    window: "7d"
    weight: 0.2
  - name: product_overlap
    method: set_overlap
    fields: ["invoice.line_items.sku[]", "usage.feature_sku[]"]
    weight: 0.2

thresholds:
  promote: 0.85
  candidate: 0.6
```

### 3. Observe health and metrics

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/metrics   # Prometheus format
```

---

## 🧠 How It Works

1. **Ingest:** connects to Stripe and your chosen usage source (read-only).  
2. **Hash:** converts sensitive identifiers into synthetic IDs using HMAC(userId, tenantSalt).  
3. **Match:** applies deterministic + probabilistic matchers defined in `recipes.yaml`.  
4. **Verify:** signs each match with a `linkProof` hash.  
5. **Emit:** sends only synthetic link edges + proofs to Connektn Cloud (via HTTPS).  

---

## 🧩 Connectors (planned roadmap)

| Connector | Type | Status |
|------------|------|--------|
| Stripe | Billing | ✅ v0.1 |
| Postgres | Usage DB | 🧩 in progress |
| Snowflake | Usage Warehouse | 🧩 in progress |
| BigQuery | Usage Warehouse | 🧩 in progress |
| PostHog | Analytics API | 🧩 in progress |
| S3/GCS | File exports | 🧩 planned |

---

## 🧮 Matchers

| Matcher | Description | Output |
|----------|--------------|---------|
| **HMAC Join** | Deterministic hash-based match on known ID fields. | confidence = 1.0 |
| **Temporal Proximity** | Correlates events by time windows (e.g., signup ↔ first invoice). | 0.5–0.9 |
| **SKU Overlap** | Compares purchased SKUs with features used. | 0.6–0.9 |
| **Similarity** | Behavioral vector similarity using cosine distance. | 0.4–0.8 |

---

## 🧰 Development

### Requirements
- Go ≥ 1.22  
- Docker (optional)  
- Make

### Build

```bash
make build
# or
go build ./cmd/linker
```

### Run locally

```bash
CONNEKTN_TENANT_KEY=dev_tenant STRIPE_API_KEY=sk_test_xxx TENANT_SALT_SECRET=local_salt go run ./cmd/linker
```

### Lint & Test

```bash
make lint test
```

---

## 🧪 Observability

The agent exposes Prometheus and OpenTelemetry endpoints:

| Endpoint | Purpose |
|-----------|----------|
| `/healthz` | basic health |
| `/metrics` | Prometheus metrics |
| `/readyz` | readiness probe |
| `/debug/pprof` | optional profiling (disabled by default) |

---

## 🔐 Security & Privacy

- **Zero ownership:** no raw PII is ever transmitted or stored.  
- **TenantSalt:** cryptographic salt generated and stored by the tenant.  
- **Signed releases:** all binaries and Docker images are signed with `cosign`.  
- **SBOM:** each release publishes a full software bill of materials.  
- **SOC 2 roadmap:** Connektn aims for certification once public beta stabilizes.

Please see [`SECURITY.md`](SECURITY.md) for responsible disclosure policy.

---

## 🤝 Contributing

We welcome issues and pull requests!  
Please read [`CONTRIBUTING.md`](CONTRIBUTING.md) before submitting.

- Run `make lint test` before pushing.  
- Sign commits with GPG or SSH key (required for maintainers).  
- Discuss larger design changes in [Discussions](https://github.com/connektn/linker-agent/discussions).

---

## 📄 License

Apache License 2.0 — see [`LICENSE`](LICENSE).

---

## 🧭 Roadmap

| Milestone | Description | ETA |
|------------|--------------|------|
| **v0.1** | Stripe + Postgres connectors, HMAC + temporal matchers, Docker image, Helm chart | Q1 2026 |
| **v0.2** | BigQuery, Snowflake connectors, OpenTelemetry dashboard | Q2 2026 |
| **v0.3** | Behavioral similarity matcher, probabilistic ensemble | Q3 2026 |
| **v1.0** | Full connector set + managed updates | Q4 2026 |

---

## 💬 Support & Contact

📫 founders@connektn.dev  
💻 [https://connektn.dev](https://connektn.dev)  
🐙 [GitHub Issues](https://github.com/connektn/linker-agent/issues)

---

> **Connektn Linker Agent** — _“We reconcile your customer truth, without ever seeing who your customers are.”_
