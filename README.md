# 🧩 Connektn Linker Agent

> **Zero-ownership CDP: Privacy-safe data reconciliation for Stripe-based SaaS businesses.**
> Runs inside your infrastructure. Produces verified, anonymized customer graphs without ever exposing PII.

---

## Table of Contents

- [Overview](#overview)
  - [What is Zero-Ownership CDP?](#what-is-zero-ownership-cdp)
  - [Synthetic ID Anonymization](#synthetic-id-anonymization)
- [Architecture](#architecture)
- [Implemented Modules](#implemented-modules)
  - [1. Configuration Loader](#1-configuration-loader-internalconfig)
  - [2. Stripe Connector](#2-stripe-connector-internalconnectorsstripe)
  - [3. Crypto & Synthetic ID System](#3-crypto--synthetic-id-system-internalcrypto)
  - [4. Models & Sanitizers](#4-models--sanitizers-internalmodels-internalconnectorsstripesanitizego)
  - [5. Per-Stream Dual Sink Exporter](#5-per-stream-dual-sink-exporter-internalexporter)
  - [6. Matcher Framework](#6-matcher-framework-internalmatchers)
  - [7. Pipeline Orchestrator](#7-pipeline-orchestrator-internalpipeline)
  - [8. Stripe Test Data Seeder](#8-stripe-test-data-seeder-scriptsseed_stripe_test_datash)
- [Implemented Features](#implemented-features)
- [Quick Start](#quick-start)
  - [Prerequisites](#prerequisites)
  - [1. Clone and Build](#1-clone-and-build)
  - [2. Configure](#2-configure)
  - [3. Seed Stripe Test Data (Optional)](#3-seed-stripe-test-data-optional)
  - [4. Set Environment Variables](#4-set-environment-variables)
  - [5. Run the Matcher Pipeline](#5-run-the-matcher-pipeline)
  - [6. Verify Exported Data](#6-verify-exported-data)
  - [7. Privacy Verification](#7-privacy-verification)
- [Live Mode with Stripe Webhooks](#live-mode-with-stripe-webhooks)
  - [Webhook Features](#webhook-features)
  - [Supported Events](#supported-events)
  - [Configuration](#configuration)
  - [Setup Instructions](#setup-instructions)
  - [How It Works](#how-it-works)
  - [Security Model](#security-model)
  - [Monitoring](#monitoring)
  - [Troubleshooting](#troubleshooting)
- [Agent Management & Remote Control](#agent-management--remote-control)
  - [Heartbeat Monitoring](#heartbeat-monitoring)
  - [Control Commands](#control-commands)
  - [Configuration](#agent-configuration)
  - [Security](#agent-security)
- [Privacy & Security Principles](#privacy--security-principles)
  - [Zero-PII Architecture](#zero-pii-architecture)
  - [Tenant Salt Management](#tenant-salt-management)
  - [Privacy Modes](#privacy-modes)
    - [Strict Mode (default)](#strict-mode-default)
    - [Passthrough Mode](#passthrough-mode)
    - [Export Guardrails](#export-guardrails)
    - [Comparison Table](#comparison-table)
- [Development](#development)
  - [Project Structure](#project-structure)
  - [Running Tests](#running-tests)
  - [Building](#building)
- [Export Formats](#export-formats)
- [Contributing](#contributing)
- [License](#license)
- [Current Status & Roadmap](#current-status--roadmap)
- [Support & Contact](#support--contact)

---

## Overview

The **Connektn Linker Agent** is the core on-premise component of the [Connektn Zero-Ownership CDP](https://connektn.io).

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

## Architecture

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

## Implemented Modules

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
    baseUrl: "https://api.connektn.io"
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
  endpoint: "https://api.connektn.io/ingest"  # Migrates to http.baseUrl
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

## Implemented Features

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

## Quick Start

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
    baseUrl: "https://api.connektn.io"
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

## Live Mode with Stripe Webhooks

The Linker Agent supports real-time processing of Stripe events via webhooks. Instead of running periodic backfills, you can configure the agent to receive webhook events and process them as they occur.

### Webhook Features

- ✅ **Secure signature verification** (HMAC-SHA256)
- ✅ **Idempotency** (duplicate events safely ignored)
- ✅ **IP allowlist** (optional additional security)
- ✅ **Retry logic** with exponential backoff
- ✅ **Zero-trust model** (always fetches canonical objects from Stripe API)
- ✅ **Same privacy guarantees** (synthetic IDs, no PII)

### Supported Events

The webhook handler processes the following Stripe event types:

- **Invoice events:** `invoice.created`, `invoice.finalized`, `invoice.payment_succeeded`, `invoice.payment_failed`
- **Subscription events:** `customer.subscription.created`, `customer.subscription.updated`, `customer.subscription.deleted`
- **Charge events** (optional): `charge.succeeded`, `charge.refunded`

Unknown event types are safely ignored with HTTP 200 response.

### Configuration

Add the webhook section to your `config.yaml`:

```yaml
sources:
  stripe:
    apiKey: "env:STRIPE_API_KEY"
    maxRequestsPerSecond: 8

    webhook:
      enabled: true
      path: "/webhooks/stripe"
      signingSecret: "env:STRIPE_WEBHOOK_SECRET"

      # Optional: IP allowlist (Stripe webhook IPs)
      allowedIPRanges:
        - "3.18.12.63/32"
        - "3.130.192.231/32"

      # Optional: timestamp tolerance (default: 300s)
      maxSkew: 300s

      # Optional: retry configuration
      retry:
        maxAttempts: 5
        baseBackoff: 2s
        maxBackoff: 30s
```

### Setup Instructions

#### 1. Get Your Webhook Signing Secret

In your Stripe Dashboard:
1. Go to **Developers** → **Webhooks**
2. Click **Add endpoint**
3. Enter your endpoint URL: `https://your-domain.com/webhooks/stripe`
4. Select events to receive (invoice.*, customer.subscription.*)
5. Copy the **Signing secret** (starts with `whsec_`)

#### 2. Configure Environment Variables

```bash
export STRIPE_API_KEY=sk_test_xxxxx
export STRIPE_WEBHOOK_SECRET=whsec_xxxxx
export TENANT_SALT=$(openssl rand -hex 32)
```

#### 3. Run the Agent

```bash
./linker-agent
```

The webhook endpoint will be available at the configured path (default: `/webhooks/stripe`).

#### 4. Test with Stripe CLI (Local Development)

For local testing, use the [Stripe CLI](https://stripe.com/docs/stripe-cli) to forward events:

```bash
# Install Stripe CLI
brew install stripe/stripe-cli/stripe  # macOS
# or download from https://stripe.com/docs/stripe-cli

# Login
stripe login

# Forward events to local agent
stripe listen --forward-to http://localhost:8080/webhooks/stripe

# In another terminal, trigger test events
stripe trigger invoice.payment_succeeded
stripe trigger customer.subscription.created
```

**Expected Output (Agent Logs):**

```
2025/10/26 08:15:23 Webhook handler started on /webhooks/stripe
2025/10/26 08:15:45 webhook event enqueued event_id=evt_test_xxx type=invoice.payment_succeeded
2025/10/26 08:15:45 webhook job processed successfully event_id=evt_test_xxx type=invoice.payment_succeeded edges_exported=2
```

### How It Works

1. **Stripe sends webhook** → Your agent's `/webhooks/stripe` endpoint
2. **Signature verification** → HMAC-SHA256 validation with `signingSecret`
3. **Idempotency check** → Duplicate `event.id` values are ignored
4. **Job enqueuing** → Event metadata queued for processing
5. **Return 200 immediately** → Stripe considers delivery successful
6. **Background processing:**
   - Fetch canonical object from Stripe API (never trust webhook payload)
   - Sanitize to synthetic IDs
   - Run matcher pipeline
   - Export link edges via configured exporter

### Security Model

**Threat Model:**

The webhook handler defends against:
- ❌ **Forged events** (signature verification required)
- ❌ **Replay attacks** (timestamp validation with `maxSkew`)
- ❌ **Unauthorized IPs** (optional IP allowlist)
- ❌ **PII in logs** (no payload logging, only metadata)

**Zero-Trust Principle:**

The agent **never trusts webhook payloads for data**. It only extracts:
- Event ID (for idempotency)
- Event type (to determine processing path)
- Object ID (to fetch canonical object)
- Timestamp (for replay protection)

All actual data comes from direct Stripe API calls with expanded fields.

### Monitoring

The webhook handler exposes metrics counters:

```go
webhook_received_total          // Total events received
webhook_verified_total          // Events that passed signature verification
webhook_dup_dropped_total       // Duplicate events ignored
webhook_enqueued_total          // Events queued for processing
webhook_errors_total{reason}    // Errors by reason (signature_invalid, queue_full, etc.)
webhook_processed_total{status} // Processing results (success, failed, ignored)
```

These can be exposed via a `/metrics` endpoint for Prometheus scraping (future enhancement).

### Troubleshooting

**Problem:** `signature verification failed`

- **Solution:** Verify `STRIPE_WEBHOOK_SECRET` matches Stripe Dashboard
- Check timestamp skew (system clock must be accurate within `maxSkew`)

**Problem:** `IP address not in allowlist`

- **Solution:** Add Stripe's webhook IPs to `allowedIPRanges` or remove the allowlist for testing
- Stripe's webhook IPs: https://stripe.com/docs/ips

**Problem:** `webhook job queue full`

- **Solution:** Increase `queueDepth` in handler options or scale worker processing
- Check if worker is blocked on Stripe API rate limits

**Problem:** Events are duplicated

- **Solution:** This should not happen due to idempotency tracking
- Check logs for `duplicate webhook event ignored` messages
- Verify event IDs are unique

---

## Agent Management & Remote Control

The Linker Agent includes built-in remote management capabilities for production deployments. Agents send periodic heartbeats to Connektn Cloud and accept signed remote control commands for operational management.

### Heartbeat Monitoring

Agents send encrypted health metrics to Connektn Cloud at configurable intervals (default: 30s):

**Heartbeat payload includes:**
- Agent ID (persistent identifier)
- Organization ID
- Uptime
- Current privacy mode (`strict` or `passthrough`)
- Queue metrics (depth, dropped count, enqueued count)
- HMAC-SHA256 signature for authenticity

**Configuration:**
```yaml
heartbeat:
  enabled: true
  endpoint: "https://api.connektn.io/agent/heartbeat"
  interval: 30s
  signatureSecret: "env:HEARTBEAT_SECRET"
```

**Environment variables:**
```bash
export HEARTBEAT_SECRET="your-heartbeat-signing-secret"
```

### Control Commands

The agent exposes a control API endpoint for remote management operations. All commands require HMAC-SHA256 signatures with nonce-based replay protection.

**Supported commands:**

| Command | Description | Effect |
|---------|-------------|--------|
| `switch_mode` | Change privacy mode | Switches between `strict` and `passthrough` modes WITHOUT restarting |
| `restart` | Graceful restart | Triggers shutdown (K8s/orchestrator restarts pod automatically) |
| `stop` | Stop agent | Gracefully stops all processing |
| `start` | Start agent | Resumes processing after stop |
| `upgrade` | Upgrade agent binary | Downloads and applies new version (not yet implemented) |

**Example: Switch privacy mode**

Mode switching happens dynamically without restarting the agent, ensuring zero downtime:

```bash
# From Connektn Cloud API or CLI
curl -X POST https://your-agent:8081/api/control/command \
  -H "Content-Type: application/json" \
  -d '{
    "command": "switch_mode",
    "params": {"mode": "passthrough"},
    "timestamp": "2025-01-15T10:30:00Z",
    "nonce": "unique-random-value",
    "signature": "hmac-sha256-signature-here"
  }'
```

### Configuration

```yaml
agent:
  organizationId: "env:ORGANIZATION_ID"
  version: "1.0.0"

control:
  enabled: true
  listenAddr: ":8081"
  signatureSecret: "env:CONTROL_SECRET"
  maxClockSkew: 5m
  nonceCache:
    ttl: 10m
```

**Environment variables:**
```bash
export ORGANIZATION_ID="org_abc123"
export CONTROL_SECRET="your-control-command-secret"
```

**Persistent Agent ID:**

The agent automatically generates a persistent identifier on first run:
- Stored in `/var/lib/connektn/agent-id` (production)
- Format: `agent_{32-char-hex}`
- Survives restarts and redeployments
- Used for heartbeat tracking and command routing

### Security

**Signature verification:**
- All control commands must include valid HMAC-SHA256 signatures
- Signature covers: `command + timestamp + nonce + params`
- Uses constant-time comparison to prevent timing attacks

**Replay protection:**
- Nonces are cached for configurable TTL (default: 10 minutes)
- Same nonce cannot be used twice
- Commands older than `maxClockSkew` are rejected

**Clock skew tolerance:**
- Default: 5 minutes
- Protects against stale/future-dated commands
- Configurable via `control.maxClockSkew`

**Transport security:**
- Control endpoint should be behind firewall/VPN in production
- Consider using mutual TLS for additional security
- Kubernetes NetworkPolicies can restrict access

---

## Privacy & Security Principles

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

### Privacy Modes

The Linker Agent supports two privacy modes for ID handling, configured via `privacy.idMode`:

#### Strict Mode (default)

**All identifiers are converted to synthetic IDs.** Raw platform IDs (e.g., Stripe `cus_`, `sub_`, `in_`) are never exported.

```yaml
privacy:
  mode: "strict"
  idMode: "strict"  # or omit (defaults to strict)
  tenantSalt: "env:TENANT_SALT"
```

**Behavior:**
- ✅ All IDs exported as `syn_cust_xxx`, `syn_sub_xxx`, etc.
- ✅ Maximum privacy: no raw platform IDs ever leak
- ✅ Tenant-isolated via HMAC with tenantSalt
- ✅ Cryptographic proofs include mode to prevent replay

**Use this mode when:**
- You want maximum privacy guarantees
- You're integrating with third-party systems
- You need to comply with strict data protection regulations

#### Passthrough Mode

**Raw platform IDs are preserved** (e.g., Stripe `cus_xxx`, `sub_xxx`), but PII/PHI is still stripped.

```yaml
privacy:
  mode: "strict"  # legacy field, still required
  idMode: "passthrough"
  allowPassthroughExports: false  # see guardrails below
```

**Behavior:**
- ✅ IDs remain as-is: `cus_xxx`, `sub_xxx`, `in_xxx`
- ✅ PII/PHI still stripped (no names, emails, addresses)
- ✅ Easier integration with existing Stripe-based systems
- ⚠️  Raw platform IDs visible in exports

**Use this mode when:**
- You need to correlate agent output with existing Stripe dashboards/tools
- Your data pipeline already handles Stripe IDs securely
- You're exporting to your own infrastructure (not third parties)

#### Export Guardrails

To prevent accidental exposure of raw IDs, passthrough mode includes startup validation:

**HTTP Export Rules:**
- ✅ Always allowed to `https://api.connektn.dev` or `https://api.connektn.io`
- ❌ Blocked to other endpoints unless `allowPassthroughExports: true`
- ⚠️  Warning banner logged on startup

**File Export:**
- ✅ Always allowed (you control the file system)
- ⚠️  Warning banner logged on startup

**Example Error (passthrough + HTTP to non-Connektn endpoint):**

```
FATAL: exporter: passthrough mode with HTTP export to non-Connektn endpoint
"https://example.com" requires privacy.allowPassthroughExports = true in config
```

**Example Config (allow passthrough to custom endpoint):**

```yaml
privacy:
  mode: "strict"
  idMode: "passthrough"
  allowPassthroughExports: true  # explicitly allow

export:
  mode: "http"
  http:
    baseUrl: "https://your-internal-api.company.com"
    routes:
      edges: "/ingest/edges"
```

#### Comparison Table

| Feature | Strict Mode | Passthrough Mode |
|---------|-------------|------------------|
| **ID Format** | `syn_cust_abc123` | `cus_abc123` (original) |
| **PII/PHI** | ❌ Never included | ❌ Never included |
| **Proofs** | Include `idMode` | Include `idMode` |
| **HTTP to Connektn** | ✅ Allowed | ✅ Allowed |
| **HTTP to other endpoints** | ✅ Allowed | ⚠️  Requires `allowPassthroughExports: true` |
| **File export** | ✅ Allowed | ✅ Allowed (with warning) |
| **Cross-mode replay** | ❌ Blocked by proof | ❌ Blocked by proof |
| **Startup logging** | ℹ️ "ID mode: strict" | ⚠️ "PASSTHROUGH MODE: exporting raw platform IDs (no PII)" |

---

## Development

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

## Export Formats

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

## Contributing

We welcome issues and pull requests!

- Run `go test ./...` before pushing
- Follow [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)
- See [`CLAUDE.md`](CLAUDE.md) for AI collaboration guidelines
- Discuss larger design changes in [Discussions](https://github.com/connektn/linker-agent/discussions)

---

## License

Apache License 2.0 — see [`LICENSE`](LICENSE).

---

## Current Status & Roadmap

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

## Support & Contact

📫 founders@connektn.io
💻 [https://connektn.io](https://connektn.io)
🐙 [GitHub Issues](https://github.com/connektn/linker-agent/issues)

---

> **Connektn Linker Agent** — _"We reconcile your customer truth, without ever seeing who your customers are."_
