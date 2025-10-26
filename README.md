# 🧩 Connektn Linker Agent

> **Zero-ownership CDP: Privacy-safe data reconciliation for Stripe-based SaaS businesses.**
> Runs inside your infrastructure. Produces verified, anonymized customer graphs without ever exposing PII.

---

## 🔍 Overview

The **Connektn Linker Agent** is the core on-premise component of the [Connektn Zero-Ownership CDP](https://connektn.dev).

### What is Zero-Ownership CDP?

Traditional CDPs collect and centralize your customer data, creating privacy risks and compliance burdens. Connektn takes a different approach:

- **Your data stays in your infrastructure** — we never see raw customer information
- **Synthetic identifiers** replace all real IDs (customers, subscriptions, invoices, products)
- **Cryptographic proofs** verify data relationships without exposing underlying details
- **Local processing** means PII never crosses your network boundary

The Linker Agent runs inside your environment (Docker or Kubernetes) and performs **local, privacy-safe matching** between:

- **Billing data** from Stripe, and
- **Feature-usage / analytics data** from your warehouse, product logs, or analytics platform

Instead of collecting raw data, the Linker Agent generates **tenant-scoped synthetic identifiers and cryptographic proofs** of reconciliation, then sends only anonymized link graphs to the Connektn Cloud (or exports them locally for analysis).

> **"Your data never leaves your infrastructure — only the math does."**

### Synthetic ID Anonymization

Every identifier exported by the Linker Agent is converted to a **synthetic ID** using HMAC-SHA256 with a tenant-specific salt:

```
syntheticID = HMAC-SHA256(tenantSalt, rawID)[:16]
formatted   = "syn_{prefix}_{16-char-hex}"
```

**Example transformations:**

| Original Stripe ID | Synthetic ID | Prefix |
|-------------------|--------------|---------|
| `cus_abc123` | `syn_cust_b70ef6426cbc4440` | `syn_cust` |
| `sub_xyz789` | `syn_sub_991e0fe981830ab6` | `syn_sub` |
| `in_invoice1` | `syn_inv_151adbe26652a030` | `syn_inv` |
| `price_plan1` | `syn_price_e200f89b17718e9a` | `syn_price` |
| `prod_product1` | `syn_prod_49329337feab512c` | `syn_prod` |

**Key properties:**
- ✅ **Deterministic:** Same ID + salt always produces same synthetic ID (enables linkage)
- ✅ **Irreversible:** Cannot derive original ID from synthetic ID (HMAC security)
- ✅ **Tenant-isolated:** Different salts produce different synthetic IDs (multi-tenant safe)
- ✅ **Collision-resistant:** 16 hex chars (64 bits) provides sufficient uniqueness

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
│     ├── internal/config/           ← ✅ YAML + env var loader
│     ├── internal/connectors/
│     │     └─ stripe/               ← ✅ Read-only Stripe API client + Lite mappers
│     ├── internal/crypto/           ← ✅ HMAC-SHA256 synthetic ID system
│     ├── internal/models/           ← ✅ Privacy-safe data models
│     ├── internal/exporter/         ← ✅ HTTP + file dual sink
│     ├── internal/matchers/         ← ✅ Ensemble framework with 3 matchers
│     ├── internal/pipeline/         ← ✅ Orchestrator + link edge generation
│     └── main.go                    ← ✅ Entrypoint with matcher pipeline
│
└─ 🌐 Connektn Cloud (ingest API) OR local file
      └── receives only synthetic links + proofs
```

---

## ⚙️ Implemented Modules

### 1. Configuration Loader (`internal/config/`)

**Purpose:** Load and validate YAML configuration with environment variable indirection.

**Features:**
- YAML parsing with `env:VAR_NAME` syntax for secrets
- Validation of required fields (API keys, salts)
- Support for multiple export modes (HTTP, file, both)

**Example:**
```yaml
privacy:
  tenantSalt: "env:TENANT_SALT"    # Reads from $TENANT_SALT
sources:
  stripe:
    apiKey: "env:STRIPE_API_KEY"  # Reads from $STRIPE_API_KEY
```

### 2. Stripe Connector (`internal/connectors/stripe/`)

**Purpose:** Privacy-safe read-only access to Stripe API.

**Features:**
- Uses official Stripe Go SDK (v83.0.0)
- Token bucket rate limiting (configurable RPS)
- Methods to fetch raw subscription and invoice data
- No PII fields are ever accessed or logged

**Key Methods:**
- `ListRawSubscriptions(ctx, customerID, limit)` → `[]*stripe.Subscription`
- `ListRawInvoices(ctx, customerID, subscriptionID, limit)` → `[]*stripe.Invoice`
- `SmokeCheck(ctx)` → Quick health check

### 3. Crypto & Synthetic ID System (`internal/crypto/`)

**Purpose:** Generate tenant-scoped synthetic identifiers for all Stripe entities.

**Core Functions:**
- `HMACSHA256Hex(salt, data)` → HMAC-SHA256 hash as hex string
- `SyntheticID(salt, rawID, prefix)` → Prefixed synthetic ID (e.g., `syn_sub_abc123...`)

**Security:**
- Uses Go's `crypto/hmac` and `crypto/sha256` packages
- Constant-time comparison to prevent timing attacks
- 16-character hex truncation for balance between uniqueness and privacy

### 4. Models & Sanitizers (`internal/models/`, `internal/connectors/stripe/sanitize.go`)

**Purpose:** Define PII-free data structures and convert raw Stripe objects to safe models.

**Models:**
- `Subscription` — subscription ID, customer ID, status, items, timestamp
- `Invoice` — invoice ID, customer ID, subscription ID, amount, currency, status
- `SubItem` — price ID, product ID, quantity

**Sanitizers:**
- `SanitizeSubscription(salt, *stripe.Subscription)` → Converts all IDs to synthetic
- `SanitizeInvoice(salt, *stripe.Invoice)` → Converts all IDs to synthetic

**What's removed:**
- ❌ Customer names, emails, addresses, phone numbers
- ❌ Payment method details (card numbers, expiry)
- ❌ Metadata, descriptions, notes
- ❌ Any free-form user input

### 5. Per-Stream Dual Sink Exporter (`internal/exporter/`)

**Purpose:** Export anonymized data to HTTP endpoint and/or local file with per-stream routing.

**Features:**
- **Per-stream routing:** Different data types (edges, billing, usage) route to distinct endpoints/files
- **HTTP mode:** POST batched JSON to Connektn Cloud with retry logic
- **File mode:** Append JSONL to local file for debugging/testing
- **Both mode:** Export to HTTP and file simultaneously
- **Environment-based auth:** Read bearer tokens from environment variables
- Exponential backoff retry (2s, 4s, 8s intervals)
- Idempotency keys (UUID v4) for safe retries
- Graceful shutdown with queue flush
- Backward compatible with legacy single-sink configuration

**Configuration:**
```yaml
export:
  mode: "both"                          # "http" | "file" | "both"

  # Per-stream HTTP configuration
  http:
    baseUrl: "https://api.connektn.dev"
    routes:
      edges: "/ingest/edges"            # Link edges endpoint
      billing: "/ingest/billing"        # Billing data endpoint
      usage: "/ingest/usage"            # Usage events endpoint
    headers:
      authorizationEnv: "CONNEKTN_TENANT_KEY"  # Optional: env var for auth token
    maxRetries: 3
    batchSize: 50
    flushEvery: 5s

  # Per-stream file configuration
  file:
    paths:
      edges: "reports/link_edges.jsonl"
      billing: "reports/billing.jsonl"
      usage: "reports/usage.jsonl"
```

**Legacy Configuration (Still Supported):**
```yaml
export:
  mode: "file"
  endpoint: "https://api.connektn.dev/ingest"  # Migrates to http.baseUrl
  filePath: "reports/exporter_output.jsonl"    # Migrates to file.paths.edges
```

### 6. Matcher Framework (`internal/matchers/`)

**Purpose:** Generate privacy-preserving link edges between usage events and billing data.

**Features:**
- **Ensemble architecture:** Combine multiple matchers with configurable weights
- **Three matchers implemented:**
  - `DeterministicIDMatcher`: Exact customer ID matches (HMAC-based)
  - `TemporalMatcher`: Time-based correlation (usage near subscription/invoice creation)
  - `SKUOverlapMatcher`: Product/price overlap analysis
- **Confidence scoring:** Weighted combination with threshold filtering
- **Cryptographic proofs:** Each link edge includes HMAC proof of its components
- **Explainability:** Human-readable notes explain why each match was made

**Key Types:**
- `UsageEvent` — synthetic user ID, feature, SKU, timestamp
- `SubscriptionLite` / `InvoiceLite` — minimal billing data for matching
- `LinkEdge` — verified connection with confidence score and proof
- `Ensemble` — orchestrates matchers and combines results

**Example Link Edge:**
```json
{
  "From": "syn_cust_6a95702fc4e99e2b",
  "To": "syn_sub_af93a5b01e0deff7",
  "Kind": "user->subscription",
  "Confidence": 1.0,
  "Proof": "89db5647058126bb5c772ec147bf000ac3b1bff395f5ad854c5779c7ac488159",
  "Recipe": "default/v1",
  "At": 1761425330,
  "Notes": "exact customer ID match; usage within 240s of subscription creation"
}
```

### 7. Pipeline Orchestrator (`internal/pipeline/`)

**Purpose:** End-to-end workflow from data fetching to link edge export.

**Features:**
- Input validation (ensures all IDs are synthetic)
- Runs all matchers in ensemble
- Combines and filters edges by confidence threshold
- Generates detailed statistics
- Exports link edges to separate file/endpoint from billing data

**Key Function:**
```go
Run(ctx, ensemble, exporter, Inputs{usages, subs, invs}) -> Result
```

### 8. Stripe Test Data Seeder (`scripts/seed_stripe_test_data.sh`)

**Purpose:** Automated generation of realistic Stripe test data for development and testing.

**Creates:**
- 50+ customers with test payment methods (`tok_visa`)
- 5+ products with monthly recurring prices
- 50+ subscriptions (mix of active and trialing)
- Automatic invoice generation by Stripe
- ~15% random refunds on historical charges
- All objects tagged with `metadata[seeded_by]=ConnektnSeeder`

**Usage:**
```bash
export STRIPE_API_KEY=sk_test_xxxxx
export NUM_CUSTOMERS=100
export NUM_PRODUCTS=10
bash scripts/seed_stripe_test_data.sh
```

**Output:**
```
📊 Summary:
   Customers:     100
   Products:      10
   Prices:        10
   Subscriptions: 100
   Invoices:      100
   Refunds:       15

💾 Reports saved in: seed_reports/
```

---

## ⚙️ Implemented Features

### ✅ Current Release (v0.3-alpha)

- 🔐 **Zero-PII Architecture:** All identifiers converted to HMAC-SHA256 synthetic IDs
- 🆔 **Synthetic ID System:** Tenant-scoped anonymization for ALL entities:
  - Customers: `cus_xxx` → `syn_cust_xxx`
  - Subscriptions: `sub_xxx` → `syn_sub_xxx`
  - Invoices: `in_xxx` → `syn_inv_xxx`
  - Prices: `price_xxx` → `syn_price_xxx`
  - Products: `prod_xxx` → `syn_prod_xxx`
- 📦 **Stripe Connector:** Read-only access with rate limiting and smoke checks
- 🔧 **Configuration Loader:** YAML-based config with `env:VAR_NAME` indirection
- 📤 **Per-Stream Dual Sink Exporter:**
  - HTTP mode: batched payloads to Connektn Cloud with per-stream routing
  - File mode: JSONL locally for testing with per-stream file paths
  - Both mode: simultaneous HTTP + file export
  - **Per-stream routing:** edges → `/ingest/edges`, billing → `/ingest/billing`, usage → `/ingest/usage`
  - **Environment-based auth:** Read bearer tokens from environment variables
  - **Backward compatible:** Legacy single-sink config auto-migrates
- 🧠 **Matcher Framework:** Ensemble with 3 matchers
  - DeterministicIDMatcher (exact HMAC joins)
  - TemporalMatcher (time-based correlation)
  - SKUOverlapMatcher (product/price overlap)
- 🔗 **Link Edge Generation:** Privacy-preserving relationships with cryptographic proofs
- 📊 **Pipeline Orchestrator:** End-to-end workflow with statistics
- ⚡ **Retry Logic:** Exponential backoff with idempotency keys
- 🧪 **Test Data Seeder:** `seed_stripe_test_data.sh` for realistic test data
- ✅ **Comprehensive Tests:** Unit tests for crypto, sanitizers, exporter, matchers, pipeline

### 🚧 Roadmap (Coming Soon)

- 🧠 **Advanced Matchers:** Behavioral similarity, fuzzy matching
- ⚙️ **Additional Connectors:** Snowflake, BigQuery, Postgres, PostHog
- 📊 **OpenTelemetry:** Metrics and tracing
- 🐳 **Docker Image:** Production-ready container
- ☸️ **Helm Chart:** Kubernetes deployment

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

  # Per-stream HTTP configuration (used when mode is "http" or "both")
  http:
    baseUrl: "https://api.connektn.dev"
    routes:
      edges: "/ingest/edges"
      billing: "/ingest/billing"
      usage: "/ingest/usage"
    headers:
      authorizationEnv: "CONNEKTN_TENANT_KEY"  # Optional: env var for bearer token
    maxRetries: 3
    batchSize: 50
    flushEvery: 5s

  # Per-stream file configuration (used when mode is "file" or "both")
  file:
    paths:
      edges: "reports/link_edges.jsonl"
      billing: "reports/billing.jsonl"
      usage: "reports/usage.jsonl"

matchers:
  recipe:
    name: "default"
    version: "v1"
    weights:
      deterministic_id: 1.0
      temporal_proximity: 0.5
      sku_overlap: 0.8
    threshold: 0.8                   # Minimum confidence to accept edge
    temporalWindowSec: 3600          # 1 hour correlation window
    skuOverlapMin: 0.5               # Minimum SKU overlap ratio
```

**Export Modes:**

- **`mode: "http"`** - Export to Connektn Cloud API only
- **`mode: "file"`** - Export to local JSONL file only (useful for testing)
- **`mode: "both"`** - Export to both HTTP and file simultaneously

**Matcher Configuration:**

- **`weights`**: Contribution of each matcher to final confidence score
- **`threshold`**: Edges below this confidence are filtered out (0.0-1.0)
- **`temporalWindowSec`**: Max time gap between usage and billing event
- **`skuOverlapMin`**: Minimum ratio of matching SKUs required

### 3. Seed Stripe Test Data (Optional)

Generate realistic test data in your Stripe test account:

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

- ✅ 50+ customers with test payment methods (`tok_visa`)
- ✅ 5+ products with monthly recurring prices
- ✅ 50+ subscriptions (mix of active and trialing)
- ✅ Invoices automatically generated by Stripe
- ✅ ~15% random refunds on historical charges
- ✅ All tagged with `metadata[seeded_by]=ConnektnSeeder`

### 4. Set Environment Variables

```bash
export STRIPE_API_KEY=sk_test_xxxxx
export TENANT_SALT=$(openssl rand -hex 32)  # Generate secure random salt
```

### 5. Run the Matcher Pipeline

```bash
go run main.go
```

**Expected Output:**

```
2025/10/26 07:39:43 Configuration loaded successfully
2025/10/26 07:39:43 Privacy mode: strict
2025/10/26 07:39:43 === Running Matcher Pipeline ===
2025/10/26 07:39:43 Stripe client initialized
2025/10/26 07:39:43 Link edges will be exported to file: reports/link_edges.jsonl
2025/10/26 07:39:43 Fetching subscriptions from Stripe...
2025/10/26 07:39:45 Retrieved 94 subscriptions
2025/10/26 07:39:45 Fetching invoices from Stripe...
2025/10/26 07:39:48 Retrieved 94 invoices
2025/10/26 07:39:48 Sanitized 94 subscriptions and 94 invoices
2025/10/26 07:39:48 Billing data will be exported to file: reports/exporter_output.jsonl
2025/10/26 07:39:50 Exported 188 billing records to reports/exporter_output.jsonl
2025/10/26 07:39:50 Generated 10 usage events
2025/10/26 07:39:50 Ensemble configured: default/v1 (threshold: 0.80)
2025/10/26 07:39:50 Running matcher pipeline...

=== Pipeline Results ===
Usage events:    10
Subscriptions:   94
Invoices:        94
Raw edges:       2122
Accepted edges:  68
  High conf:     68 (≥0.9)
  Medium conf:   0 (0.6-0.9)
  Low conf:      0 (<0.6)
Exported edges:  68

Per-matcher edge counts:
  deterministic_id: 30
  temporal_proximity: 1880
  sku_overlap: 212

✅ Pipeline run complete
   • Billing data: reports/exporter_output.jsonl
   • Link edges:   reports/link_edges.jsonl
```

**Legacy Mode (Export Billing Only):**

If you want to skip matchers and only export billing data:
```bash
go run main.go --export-billing-only
```

### 6. Verify Exported Data

**Billing Data:**
```bash
# Count billing records
cat reports/exporter_output.jsonl | jq -s 'flatten | length'

# View sample subscription (all IDs are synthetic)
cat reports/exporter_output.jsonl | jq -s 'flatten | .[0]'
```

**Link Edges:**
```bash
# Count total edges
cat reports/link_edges.jsonl | jq 'if type == "array" then length else 1 end' | awk '{sum+=$1} END {print sum}'

# View sample edge
cat reports/link_edges.jsonl | jq -s 'flatten | .[0]'

# Count edges by type
cat reports/link_edges.jsonl | jq -r 'if type == "array" then .[] else . end | .Kind' | sort | uniq -c
```

**Sample Billing Data:**

```json
{
  "id": "syn_sub_991e0fe981830ab6",
  "customer": "syn_cust_b70ef6426cbc4440",
  "status": "trialing",
  "items": [
    {
      "price_id": "syn_price_e200f89b17718e9a",
      "product_id": "syn_prod_49329337feab512c",
      "quantity": 1
    }
  ],
  "created_at": 1761424864
}
```

**Sample Link Edge:**

```json
{
  "From": "syn_cust_6a95702fc4e99e2b",
  "To": "syn_sub_af93a5b01e0deff7",
  "Kind": "user->subscription",
  "Confidence": 1.0,
  "Proof": "89db5647058126bb5c772ec147bf000ac3b1bff395f5ad854c5779c7ac488159",
  "Recipe": "default/v1",
  "At": 1761425330,
  "Notes": "SKU matched subscription price; exact customer ID match; usage within 240s of subscription creation"
}
```

**Notice:** ALL identifiers are synthetic — no raw Stripe IDs present!

### 7. Privacy Verification

```bash
# Verify no email addresses in output
cat reports/exporter_output.jsonl | grep -i email || echo "✅ No emails found"

# Verify no PII fields
cat reports/exporter_output.jsonl | jq '.' | grep -iE '(name|email|phone|address)' || echo "✅ No PII found"

# Verify no raw Stripe IDs
cat reports/exporter_output.jsonl | jq -s 'flatten | .[].id' | grep -E '^"(cus_|sub_|in_|price_|prod_)' || echo "✅ No raw Stripe IDs found"
```

---

## 🔐 Privacy & Security Principles

### Zero-PII Architecture

The Linker Agent is designed from the ground up to **never expose personally identifiable information (PII)**:

#### 1. Synthetic IDs for ALL Entities

All identifiers are replaced with HMAC-SHA256 hashes using a tenant-specific salt:

```
syntheticID = HMAC-SHA256(tenantSalt, originalID)[:16]
prefix      = syn_{entity_type}_
result      = prefix + syntheticID
```

**Entities covered:**
- ✅ Customers (`syn_cust_`)
- ✅ Subscriptions (`syn_sub_`)
- ✅ Invoices (`syn_inv_`)
- ✅ Prices (`syn_price_`)
- ✅ Products (`syn_prod_`)
- 🚧 Charges (`syn_ch_`) — coming soon
- 🚧 Refunds (`syn_ref_`) — coming soon

#### 2. No PII Fields

The following fields are **never** collected or exported:

- ❌ Customer names
- ❌ Email addresses
- ❌ Phone numbers
- ❌ Physical addresses
- ❌ Payment method details (card numbers, expiry dates, CVV)
- ❌ IP addresses
- ❌ User agent strings
- ❌ Free-form metadata or descriptions

#### 3. Local Processing

All hashing and ID generation happens **inside your infrastructure**. Raw data never leaves your environment.

#### 4. Verifiable Privacy

You can audit the exported data yourself:

```bash
# Verify no email addresses in output
cat reports/exporter_output.jsonl | grep -i email || echo "✅ No emails found"

# Verify no PII fields
cat reports/exporter_output.jsonl | jq '.' | grep -iE '(name|email|phone|address)' || echo "✅ No PII found"

# Verify all IDs are synthetic
cat reports/exporter_output.jsonl | jq -s 'flatten | map(select(has("id"))) | .[].id' | head -10
# Output: "syn_sub_991e0fe981830ab6", "syn_inv_151adbe26652a030", ...
```

### Tenant Salt Management

The `tenantSalt` is a cryptographic secret that ensures synthetic IDs are:

- **Unique per tenant:** Same customer ID from different tenants produces different hashes
- **Deterministic:** Same customer ID always produces the same hash (enables linkage)
- **Irreversible:** Cannot derive original customer ID from the hash

**Best Practices:**

```bash
# Generate a secure random salt
openssl rand -hex 32

# Store in secure secret management
# - AWS Secrets Manager
# - HashiCorp Vault
# - Kubernetes Secrets
# - Environment variables (for development only)
```

**Important:**
- ⚠️ Never commit to version control
- ⚠️ Rotate carefully (requires re-processing historical data)
- ⚠️ Use different salts for different environments (dev/staging/prod)

---

## 🧰 Development

### Project Structure

```
linker-agent/
├── main.go                        # Entrypoint with matcher pipeline
├── config.yaml                    # Configuration file
├── internal/
│   ├── config/                    # YAML + env loader
│   │   ├── config.go
│   │   └── config_test.go
│   ├── connectors/
│   │   └── stripe/                # Stripe API connector
│   │       ├── stripe.go          # Client + rate limiting
│   │       ├── sanitize.go        # ID sanitization
│   │       ├── to_lite.go         # Stripe → Lite mappers
│   │       ├── sanitize_test.go   # Sanitizer tests
│   │       └── stripe_test.go     # Client tests
│   ├── crypto/
│   │   ├── hmac.go                # HMAC-SHA256 helpers
│   │   ├── id.go                  # Synthetic ID generation
│   │   └── hmac_test.go           # Crypto tests
│   ├── models/
│   │   └── models.go              # Privacy-safe models
│   ├── matchers/                  # Matcher framework
│   │   ├── matcher.go             # Interface + ensemble
│   │   ├── deterministic.go       # HMAC exact ID matcher
│   │   ├── temporal.go            # Time-based correlation
│   │   ├── sku_overlap.go         # Product/price overlap
│   │   └── *_test.go              # Matcher tests
│   ├── pipeline/                  # Pipeline orchestrator
│   │   ├── pipeline.go            # Main orchestration logic
│   │   └── pipeline_test.go       # Pipeline tests
│   └── exporter/
│       ├── exporter.go            # Dual sink exporter
│       ├── exporter_test.go       # Exporter tests
│       └── queue.go               # Buffered queue
├── prompts/                       # Implementation specs
│   ├── matcher-framework.md
│   └── wire-matcher-pipeline.md
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
go test ./internal/crypto/
go test ./internal/connectors/stripe/
go test ./internal/exporter/
```

**Expected Output:**

```
?     linker-agent                          [no test files]
ok    linker-agent/internal/config          0.253s
ok    linker-agent/internal/connectors/stripe  2.464s
ok    linker-agent/internal/crypto          0.350s
ok    linker-agent/internal/exporter        9.581s
?     linker-agent/internal/models          [no test files]
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
[{"id":"syn_sub_991e0fe981830ab6","customer":"syn_cust_b70ef6426cbc4440","status":"active"},{"id":"syn_inv_151adbe26652a030","customer":"syn_cust_b70ef6426cbc4440","status":"paid"}]
[{"id":"syn_sub_1fb99a8d820b0566","customer":"syn_cust_ab3ca2215f0614a7","status":"trialing"}]
```

### HTTP Payload (Cloud Mode)

Batched arrays sent as POST requests:

```json
[
  {
    "id": "syn_sub_991e0fe981830ab6",
    "customer": "syn_cust_b70ef6426cbc4440",
    "status": "trialing",
    "items": [
      {
        "price_id": "syn_price_e200f89b17718e9a",
        "product_id": "syn_prod_49329337feab512c",
        "quantity": 1
      }
    ],
    "created_at": 1761424864
  }
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

### ✅ v0.3-alpha (Current)

- [x] Configuration loader with env var support
- [x] Stripe connector (customers, subscriptions, invoices)
- [x] **Synthetic ID system for ALL entities**
- [x] HMAC-SHA256 cryptographic hashing
- [x] Dual sink exporter (HTTP + file)
- [x] **Matcher framework with ensemble architecture**
- [x] **DeterministicIDMatcher** (HMAC exact joins)
- [x] **TemporalMatcher** (time-based correlation)
- [x] **SKUOverlapMatcher** (product/price overlap)
- [x] **Pipeline orchestrator** (end-to-end workflow)
- [x] **Link edge generation** with cryptographic proofs
- [x] Retry logic with exponential backoff
- [x] Test data seeder script
- [x] Privacy verification tooling
- [x] Comprehensive test coverage (crypto, matchers, pipeline)

### 🚧 v0.4 (Next)

- [ ] Behavioral similarity matcher (cosine similarity)
- [ ] Postgres connector
- [ ] OpenTelemetry metrics and tracing
- [ ] Docker image + Helm chart
- [ ] Real usage data connectors (warehouse/analytics)

### 📅 v0.5+

- [ ] BigQuery connector
- [ ] Snowflake connector
- [ ] PostHog connector
- [ ] Advanced fuzzy matching
- [ ] Web UI for match review and debugging

---

## 💬 Support & Contact

📫 founders@connektn.dev
💻 [https://connektn.dev](https://connektn.dev)
🐙 [GitHub Issues](https://github.com/connektn/linker-agent/issues)

---

> **Connektn Linker Agent** — _"We reconcile your customer truth, without ever seeing who your customers are."_
