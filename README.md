# 🧩 Connektn Linker Agent

> **Privacy-safe data reconciliation engine for Stripe-based SaaS businesses.**
> Runs inside your infrastructure. Produces verified, anonymized customer graphs without ever exposing PII.

---

## 🔍 Overview

The **Connektn Linker Agent** is the core on-premise component of the [Connektn Zero-Ownership CDP](https://connektn.dev).
It runs inside your environment (Docker or Kubernetes) and performs **local, privacy-safe matching** between:

- **Billing data** from Stripe, and
- **Feature-usage / analytics data** from your warehouse, product logs, or analytics platform.

Instead of collecting raw data, the Linker Agent generates **synthetic identifiers and cryptographic proofs** of reconciliation, then sends only anonymized link graphs to the Connektn Cloud (or exports them locally).

> "Your data never leaves your infrastructure — only the math does."

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
│     ├── internal/config/           ← YAML + env var loader
│     ├── internal/connectors/
│     │     └─ stripe/               ← ✅ Implemented
│     ├── internal/crypto/           ← ✅ HMAC-SHA256 synthetic IDs
│     ├── internal/models/           ← ✅ Privacy-safe data models
│     ├── internal/exporter/         ← ✅ HTTP + file dual sink
│     ├── internal/matchers/         ← 🚧 Coming soon
│     └── main.go                    ← ✅ Entrypoint
│
└─ 🌐 Connektn Cloud (ingest API) OR local file
      └── receives only synthetic links + proofs
```

---

## ⚙️ Implemented Features

### ✅ Current Release (v0.1-alpha)

- 🔐 **Zero-PII Architecture:** All customer identifiers are converted to HMAC-SHA256 synthetic IDs using tenant-specific salt
- 📦 **Stripe Connector:** Read-only access to customers, subscriptions, and invoices with rate limiting
- 🔧 **Configuration Loader:** YAML-based config with `env:VAR_NAME` indirection for secrets
- 📤 **Dual Sink Exporter:**
  - HTTP mode: sends batched payloads to Connektn Cloud
  - File mode: writes JSONL locally for testing/debugging
  - Both mode: exports to HTTP and file simultaneously
- ⚡ **Retry Logic:** Exponential backoff with configurable max retries
- 🧪 **Test Data Seeder:** Automated Stripe test data generation script (`scripts/seed_stripe_test_data.sh`)

### 🚧 Roadmap (Coming Soon)

- 🧠 **Matchers:** HMAC joins, temporal proximity, SKU overlap, behavioral similarity
- 🧾 **Proof-of-Truth:** Cryptographic proof generation for each match
- ⚙️ **Additional Connectors:** Snowflake, BigQuery, Postgres, PostHog
- 📊 **OpenTelemetry:** Metrics and tracing
- 🐳 **Docker Image:** Production-ready container

---

## 🚀 Quick Start

### Prerequisites

- Go ≥ 1.22
- Stripe test account with API key
- (Optional) `jq` for JSON processing

### 1. Clone and Build

```bash
git clone https://github.com/connektn/linker-agent.git
cd linker-agent
go build -o linker-agent main.go
```

### 2. Configure

Create or edit `config.yaml`:

```yaml
server:
  addr: ":8080"

privacy:
  mode: "strict"                    # strict | standard
  tenantSalt: "env:TENANT_SALT"     # Use env var indirection

sources:
  stripe:
    apiKey: "env:STRIPE_API_KEY"    # sk_test_xxx or sk_live_xxx
    account: ""                      # Optional: connected account ID
    maxRequestsPerSecond: 8          # Rate limiting

export:
  mode: "file"                       # "http" | "file" | "both"
  endpoint: "https://api.connektn.dev/ingest"  # Required if mode includes "http"
  filePath: "reports/exporter_output.jsonl"    # Required if mode includes "file"
```

**Configuration Modes:**

- **`mode: "http"`** - Export to Connektn Cloud API only
- **`mode: "file"`** - Export to local JSONL file only (useful for testing)
- **`mode: "both"`** - Export to both HTTP and file simultaneously

### 3. Set Environment Variables

```bash
export STRIPE_API_KEY=sk_test_xxxxx
export TENANT_SALT=your-secret-salt-value
```

### 4. Run the Agent

```bash
go run main.go
```

**Expected Output:**

```
2025/10/25 22:45:34 Configuration loaded successfully
2025/10/25 22:45:34 Privacy mode: strict
2025/10/25 22:45:34 Stripe client initialized
2025/10/25 22:45:35 Stripe smoke check passed: found 50 customers
2025/10/25 22:45:35 Exporter initialized (mode: file)
2025/10/25 22:45:35 Fetching subscriptions from Stripe...
2025/10/25 22:45:35 Retrieved 50 subscriptions
2025/10/25 22:45:35 Fetching invoices from Stripe...
2025/10/25 22:45:39 Retrieved 50 invoices
2025/10/25 22:45:39 Processed 50 subscriptions with synthetic IDs
2025/10/25 22:45:39 Processed 50 invoices with synthetic IDs
2025/10/25 22:45:39 Enqueued 100 items total
✅ Exporter run finished — output captured in reports/exporter_output.jsonl
```

### 5. Verify Exported Data

```bash
# Count exported items
cat reports/exporter_output.jsonl | jq -s 'flatten | length'

# View sample subscription (with synthetic customer ID)
cat reports/exporter_output.jsonl | jq -s '.[0][0]'
```

**Sample Output:**

```json
{
  "id": "sub_1SMDwAIdkv43nOTxDsp1Ta4Z",
  "customer": "b5736b4580a2b7ce174b104924da4b37de314de64b37ae20fe9004c5bf1ee497",
  "status": "trialing",
  "items": [
    {
      "price_id": "price_1SMDr8Idkv43nOTxwRR8nIPc",
      "product_id": "prod_TIpWYUTOkozTj0",
      "quantity": 1
    }
  ],
  "created_at": 1761423582
}
```

Notice: `customer` field contains a SHA-256 hash, not the original Stripe customer ID.

---

## 🧪 Seeding Test Data

For testing and development, use the included Stripe test data seeder:

```bash
# Set your Stripe test API key
export STRIPE_API_KEY=sk_test_xxxxx

# Run with default settings (50 customers, 5 products)
bash scripts/seed_stripe_test_data.sh

# Or customize via environment variables
export NUM_CUSTOMERS=100
export NUM_PRODUCTS=10
export SUBSCRIBE_PROBABILITY=80  # 80% of customers get subscriptions
bash scripts/seed_stripe_test_data.sh
```

**What it creates:**

- ✅ 50 customers (with test payment methods attached via `tok_visa`)
- ✅ 5 products with monthly recurring prices
- ✅ ~50 subscriptions (mix of active and trialing status)
- ✅ Invoices automatically generated by Stripe
- ✅ ~15% of historical charges refunded randomly
- ✅ All objects tagged with `metadata[seeded_by]=ConnektnSeeder` for easy cleanup

**Output:**

```
📊 Summary:
   Customers:     50
   Products:      5
   Prices:        5
   Subscriptions: 50
   Invoices:      50
   Refunds:       7

💾 Reports saved in: seed_reports/
   - summary.json
   - customers.txt
   - subscriptions.txt
   - price_ids.txt

🎯 All objects tagged with metadata[seeded_by]=ConnektnSeeder for easy cleanup
```

---

## 🔐 Privacy & Security Principles

### Zero-PII Architecture

The Linker Agent is designed from the ground up to **never expose personally identifiable information (PII)**:

1. **Synthetic IDs:** All customer identifiers are replaced with HMAC-SHA256 hashes:
   ```
   syntheticID = HMAC-SHA256(tenantSalt, originalCustomerID)
   ```

2. **No PII Fields:** The following fields are **never** collected or exported:
   - Customer names
   - Email addresses
   - Phone numbers
   - Physical addresses
   - Payment method details (card numbers, expiry dates)

3. **Local Processing:** All hashing and ID generation happens **inside your infrastructure**. Raw data never leaves your environment.

4. **Verifiable Privacy:** You can audit the exported data yourself:
   ```bash
   # Verify no email addresses in output
   cat reports/exporter_output.jsonl | grep -i email || echo "✅ No emails found"

   # Verify no PII fields
   cat reports/exporter_output.jsonl | jq '.' | grep -iE '(name|email|phone|address)' || echo "✅ No PII found"
   ```

### Tenant Salt Management

The `tenantSalt` is a cryptographic secret that ensures synthetic IDs are:
- **Unique per tenant:** Same customer ID from different tenants produces different hashes
- **Deterministic:** Same customer ID always produces the same hash (enables linkage)
- **Irreversible:** Cannot derive original customer ID from the hash

**Best Practices:**

- Generate with: `openssl rand -hex 32`
- Store in: AWS Secrets Manager, HashiCorp Vault, or Kubernetes Secrets
- Never commit to version control
- Rotate carefully (requires re-processing historical data)

---

## 🧰 Development

### Project Structure

```
linker-agent/
├── main.go                        # Entrypoint
├── config.yaml                    # Configuration file
├── internal/
│   ├── config/                    # YAML + env loader
│   │   ├── config.go
│   │   └── config_test.go
│   ├── connectors/
│   │   └── stripe/                # Stripe API connector
│   │       ├── client.go          # Client + rate limiting
│   │       ├── fetch.go           # List operations
│   │       ├── models.go          # Raw Stripe models
│   │       └── stripe_test.go
│   ├── crypto/
│   │   ├── hmac.go                # HMAC-SHA256 helpers
│   │   └── hmac_test.go
│   ├── models/
│   │   └── models.go              # Privacy-safe models
│   └── exporter/
│       ├── exporter.go            # Dual sink exporter
│       ├── exporter_test.go
│       └── queue.go               # Buffered queue
└── scripts/
    └── seed_stripe_test_data.sh   # Test data generator
```

### Running Tests

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run specific package tests
go test ./internal/config/
go test ./internal/exporter/
```

### Building

```bash
# Development build
go build -o linker-agent main.go

# Production build with optimizations
go build -ldflags="-s -w" -o linker-agent main.go
```

---

## 📊 Export Formats

### JSONL (File Mode)

Each line contains a JSON array of batched items:

```jsonl
[{"id":"sub_123","customer":"a1b2c3...","status":"active"},{"id":"in_456","customer":"a1b2c3...","status":"paid"}]
[{"id":"sub_789","customer":"d4e5f6...","status":"trialing"}]
```

### HTTP Payload (Cloud Mode)

Batched arrays sent as POST requests:

```json
[
  {
    "id": "sub_1SMDwAIdkv43nOTxDsp1Ta4Z",
    "customer": "b5736b4580a2b7ce174b104924da4b37de314de64b37ae20fe9004c5bf1ee497",
    "status": "trialing",
    "items": [...],
    "created_at": 1761423582
  },
  ...
]
```

**Headers:**
- `Authorization: Bearer <tenant-key>`
- `Content-Type: application/json`
- `Idempotency-Key: <uuid-v4>` (for safe retries)

---

## 🤝 Contributing

We welcome issues and pull requests!

- Run `go test ./...` before pushing
- Follow [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)
- See [`CLAUDE.md`](CLAUDE.md) for AI collaboration guidelines
- Discuss larger design changes in [Discussions](https://github.com/connektn/linker-agent/discussions)

---

## 📄 License

Apache License 2.0 — see [`LICENSE`](LICENSE).

---

## 🧭 Current Status & Roadmap

### ✅ v0.1-alpha (Current)

- [x] Configuration loader with env var support
- [x] Stripe connector (customers, subscriptions, invoices)
- [x] HMAC-SHA256 synthetic ID generation
- [x] Dual sink exporter (HTTP + file)
- [x] Retry logic with exponential backoff
- [x] Test data seeder script
- [x] Privacy verification tooling

### 🚧 v0.2 (Next)

- [ ] HMAC matcher implementation
- [ ] Temporal proximity matcher
- [ ] Postgres connector
- [ ] OpenTelemetry metrics
- [ ] Docker image + Helm chart

### 📅 v0.3+

- [ ] BigQuery connector
- [ ] Snowflake connector
- [ ] SKU overlap matcher
- [ ] Behavioral similarity matcher
- [ ] Web UI for match review

---

## 💬 Support & Contact

📫 founders@connektn.dev
💻 [https://connektn.dev](https://connektn.dev)
🐙 [GitHub Issues](https://github.com/connektn/linker-agent/issues)

---

> **Connektn Linker Agent** — _"We reconcile your customer truth, without ever seeing who your customers are."_
