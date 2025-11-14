# Production Agent Management - Verification Steps

This guide provides step-by-step verification procedures to confirm that all agent management features are working correctly in production mode.

---

## Prerequisites

```bash
# Build the production agent
make build

# Verify build
ls -lh dist/linker-agent
```

---

## Verification 1: Agent Initialization & Persistent ID

**What this verifies:** Agent generates and persists a unique identifier across restarts.

### Step 1: First Run

```bash
# Clean any existing agent ID
rm -f /var/lib/connektn/agent-id 2>/dev/null || true
rm -f /tmp/test-agent-id 2>/dev/null || true

# Create minimal config
cat > /tmp/verify-config.yaml << 'EOF'
server:
  addr: ":8080"

privacy:
  mode: "strict"
  tenantSalt: "test-salt-verification"

agent:
  organizationId: "org_verify_test"
  version: "1.0.0"

heartbeat:
  enabled: false

control:
  enabled: false

sources:
  stripe:
    apiKey: "sk_test_fake_key_for_verification"
    webhook:
      enabled: false

export:
  mode: "file"
  file:
    paths:
      edges: "/tmp/verify-edges.jsonl"

matchers:
  recipe:
    name: "default"
    version: "v1"
EOF

# Run agent briefly (will start webhook server, Ctrl+C after you see agent ID)
# OR use background process with sleep
(./dist/linker-agent -config /tmp/verify-config.yaml 2>&1 | grep -i "agent id" &) && sleep 2 && pkill -f "linker-agent.*verify-config"
```

**Expected output:**
```
Agent ID: agent_<32-character-hex-string>
```

### Step 2: Verify Persistence

```bash
# Check agent ID file was created
cat /var/lib/connektn/agent-id

# Run agent again - should use SAME ID
(./dist/linker-agent -config /tmp/verify-config.yaml 2>&1 | grep -i "agent id" &) && sleep 2 && pkill -f "linker-agent.*verify-config"
```

**Expected:** Same agent ID as first run

### Step 3: Verify ID Format

```bash
# Extract and validate format
AGENT_ID=$(cat /var/lib/connektn/agent-id)
echo "Agent ID: $AGENT_ID"

# Verify format: agent_<32 hex chars>
if [[ $AGENT_ID =~ ^agent_[0-9a-f]{32}$ ]]; then
  echo "✅ Agent ID format is valid"
else
  echo "❌ Agent ID format is invalid"
fi
```

**Expected:** ✅ Agent ID format is valid

---

## Verification 2: Heartbeat Transmission

**What this verifies:** Agent sends signed heartbeat payloads with correct metrics.

### Step 1: Start Mock Heartbeat Receiver

In **Terminal 1:**

```bash
# Start simple HTTP server to receive heartbeats
cat > /tmp/heartbeat-receiver.go << 'EOF'
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

type Heartbeat struct {
	AgentID       string `json:"agentId"`
	OrgID         string `json:"organizationId"`
	Version       string `json:"version"`
	Uptime        int64  `json:"uptime"`
	Mode          string `json:"mode"`
	QueueDepth    int    `json:"queueDepth"`
	DLQSize       int    `json:"dlqSize"`
	DroppedCount  uint64 `json:"droppedCount"`
	EnqueuedCount uint64 `json:"enqueuedCount"`
	Signature     string `json:"signature"`
	Timestamp     int64  `json:"timestamp"`
}

func verifySignature(hb Heartbeat, secret string) bool {
	// Reconstruct payload
	payload := fmt.Sprintf("%s:%s:%d:%d:%s:%d:%d:%d:%d",
		hb.AgentID, hb.OrgID, hb.Timestamp, hb.Uptime,
		hb.Mode, hb.QueueDepth, hb.DroppedCount, hb.EnqueuedCount, hb.DLQSize)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(hb.Signature), []byte(expected))
}

func main() {
	secret := "test-heartbeat-secret"

	http.HandleFunc("/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		var hb Heartbeat
		if err := json.Unmarshal(body, &hb); err != nil {
			log.Printf("❌ Invalid JSON: %v", err)
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Verify signature
		if !verifySignature(hb, secret) {
			log.Printf("❌ Invalid signature")
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}

		log.Printf("✅ Heartbeat received:")
		log.Printf("   Agent: %s", hb.AgentID)
		log.Printf("   Org: %s", hb.OrgID)
		log.Printf("   Uptime: %ds", hb.Uptime)
		log.Printf("   Mode: %s", hb.Mode)
		log.Printf("   Queue: depth=%d dropped=%d enqueued=%d dlq=%d",
			hb.QueueDepth, hb.DroppedCount, hb.EnqueuedCount, hb.DLQSize)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	log.Println("Mock heartbeat receiver listening on :9000")
	log.Fatal(http.ListenAndServe(":9000", nil))
}
EOF

go run /tmp/heartbeat-receiver.go
```

### Step 2: Start Agent with Heartbeat Enabled

In **Terminal 2:**

```bash
# Create config with heartbeat enabled
cat > /tmp/verify-heartbeat.yaml << 'EOF'
server:
  addr: ":8080"

privacy:
  mode: "strict"
  tenantSalt: "test-salt-verification"

agent:
  organizationId: "org_verify_test"
  version: "1.0.0"

heartbeat:
  enabled: true
  endpoint: "http://localhost:9000/heartbeat"
  interval: 5s
  signatureSecret: "test-heartbeat-secret"

control:
  enabled: false

sources:
  stripe:
    apiKey: "sk_test_fake_key"
    webhook:
      enabled: true
      path: "/webhooks/stripe"
      signingSecret: "whsec_fake"

export:
  mode: "file"
  file:
    paths:
      edges: "/tmp/verify-edges.jsonl"

matchers:
  recipe:
    name: "default"
    version: "v1"
EOF

# Run agent
./dist/linker-agent -config /tmp/verify-heartbeat.yaml
```

### Step 3: Verify Heartbeat Reception

In **Terminal 1**, you should see heartbeats arriving every 5 seconds:

**Expected output:**
```
✅ Heartbeat received:
   Agent: agent_<hex>
   Org: org_verify_test
   Uptime: 5s
   Mode: strict
   Queue: depth=0 dropped=0 enqueued=0 dlq=0

✅ Heartbeat received:
   Agent: agent_<hex>
   Org: org_verify_test
   Uptime: 10s
   Mode: strict
   Queue: depth=0 dropped=0 enqueued=0 dlq=0
```

**Verification checklist:**
- ✅ Heartbeats arrive every 5 seconds
- ✅ Signature verification passes
- ✅ Agent ID is consistent
- ✅ Uptime increments correctly
- ✅ Mode shows "strict"
- ✅ Queue metrics are present

Stop the agent (Ctrl+C) when verified.

---

## Verification 3: Control Command Endpoint

**What this verifies:** Control server accepts and executes signed commands.

### Step 1: Start Agent with Control Enabled

In **Terminal 1:**

```bash
cat > /tmp/verify-control.yaml << 'EOF'
server:
  addr: ":8080"

privacy:
  mode: "strict"
  tenantSalt: "test-salt-verification"

agent:
  organizationId: "org_verify_test"
  version: "1.0.0"

heartbeat:
  enabled: false

control:
  enabled: true
  listenAddr: ":8081"
  signatureSecret: "test-control-secret-key"
  maxClockSkew: 5m
  nonceCache:
    ttl: 10m

sources:
  stripe:
    apiKey: "sk_test_fake_key"
    webhook:
      enabled: true
      path: "/webhooks/stripe"
      signingSecret: "whsec_fake"

export:
  mode: "file"
  file:
    paths:
      edges: "/tmp/verify-edges.jsonl"

matchers:
  recipe:
    name: "default"
    version: "v1"
EOF

./dist/linker-agent -config /tmp/verify-control.yaml
```

**Expected startup:**
```
Control server listening on :8081
```

### Step 2: Test Health Endpoint

In **Terminal 2:**

```bash
# Health check (no signature required)
curl -s http://localhost:8081/healthz | jq .
```

**Expected output:**
```json
{
  "status": "ok"
}
```

### Step 3: Create Command Signing Utility

```bash
cat > /tmp/send-control-command.sh << 'EOF'
#!/bin/bash

COMMAND="$1"
PARAMS="$2"
SECRET="${CONTROL_SECRET:-test-control-secret}"

TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
NONCE=$(uuidgen 2>/dev/null || cat /proc/sys/kernel/random/uuid)

# Build params JSON
if [ -z "$PARAMS" ]; then
  PARAMS_JSON="{}"
else
  PARAMS_JSON="$PARAMS"
fi

# Create signature payload: command + timestamp + nonce + params
PAYLOAD="${COMMAND}${TIMESTAMP}${NONCE}${PARAMS_JSON}"
SIGNATURE=$(echo -n "$PAYLOAD" | openssl dgst -sha256 -hmac "$SECRET" | awk '{print $2}')

# Send command
curl -X POST http://localhost:8081/api/control/command \
  -H "Content-Type: application/json" \
  -d "{
    \"command\": \"$COMMAND\",
    \"params\": $PARAMS_JSON,
    \"timestamp\": \"$TIMESTAMP\",
    \"nonce\": \"$NONCE\",
    \"signature\": \"$SIGNATURE\"
  }" | jq .
EOF

chmod +x /tmp/send-control-command.sh
```

### Step 4: Test Invalid Signature

```bash
# Send command with invalid signature
curl -X POST http://localhost:8081/api/control/command \
  -H "Content-Type: application/json" \
  -d '{
    "command": "restart",
    "timestamp": "'$(date -u +"%Y-%m-%dT%H:%M:%SZ")'",
    "nonce": "'$(uuidgen)'",
    "signature": "invalid-signature-here"
  }' | jq .
```

**Expected output:**
```json
{
  "command": "restart",
  "success": false,
  "message": "Verification failed: signature verification failed",
  "timestamp": "..."
}
```

### Step 5: Test Valid Command (Switch Mode)

```bash
# Switch to passthrough mode
/tmp/send-control-command.sh switch_mode '{"mode":"passthrough"}'
```

**Expected output:**
```json
{
  "command": "switch_mode",
  "success": true,
  "message": "Command executed successfully",
  "timestamp": "..."
}
```

**In Terminal 1** (agent logs), you should see:
```
switching privacy mode from=strict to=passthrough
```

### Step 6: Verify Mode Changed

```bash
# Switch back to strict
/tmp/send-control-command.sh switch_mode '{"mode":"strict"}'
```

**Expected:** Success response and agent logs show mode change

### Step 7: Test Replay Protection

```bash
# Send same command twice quickly
NONCE=$(uuidgen)
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
PARAMS='{"mode":"passthrough"}'
PAYLOAD="switch_mode${TIMESTAMP}${NONCE}${PARAMS}"
SIGNATURE=$(echo -n "$PAYLOAD" | openssl dgst -sha256 -hmac "test-control-secret-key" | awk '{print $2}')

# First attempt - should succeed
curl -X POST http://localhost:8081/api/control/command \
  -H "Content-Type: application/json" \
  -d "{
    \"command\": \"switch_mode\",
    \"params\": $PARAMS,
    \"timestamp\": \"$TIMESTAMP\",
    \"nonce\": \"$NONCE\",
    \"signature\": \"$SIGNATURE\"
  }" | jq .

# Second attempt with SAME nonce - should fail
curl -X POST http://localhost:8081/api/control/command \
  -H "Content-Type: application/json" \
  -d "{
    \"command\": \"switch_mode\",
    \"params\": $PARAMS,
    \"timestamp\": \"$TIMESTAMP\",
    \"nonce\": \"$NONCE\",
    \"signature\": \"$SIGNATURE\"
  }" | jq .
```

**Expected:**
- First: `"success": true`
- Second: `"success": false, "message": "...nonce already seen..."`

---

## Verification 4: Mode Switching Without Restart

**What this verifies:** Privacy mode changes dynamically without agent restart.

### Prerequisites: Agent running with heartbeat + control enabled

```bash
cat > /tmp/verify-mode-switch.yaml << 'EOF'
server:
  addr: ":8080"

privacy:
  mode: "strict"
  tenantSalt: "test-salt-verification"

agent:
  organizationId: "org_verify_test"
  version: "1.0.0"

heartbeat:
  enabled: true
  endpoint: "http://localhost:9000/heartbeat"
  interval: 5s
  signatureSecret: "test-heartbeat-secret"

control:
  enabled: true
  listenAddr: ":8081"
  signatureSecret: "test-control-secret-key"
  maxClockSkew: 5m

sources:
  stripe:
    apiKey: "sk_test_fake_key"
    webhook:
      enabled: true
      path: "/webhooks/stripe"
      signingSecret: "whsec_fake"

export:
  mode: "file"
  file:
    paths:
      edges: "/tmp/verify-edges.jsonl"

matchers:
  recipe:
    name: "default"
    version: "v1"
EOF
```

### Step 1: Start Mock Heartbeat Receiver (Terminal 1)

```bash
go run /tmp/heartbeat-receiver.go
```

### Step 2: Start Agent (Terminal 2)

```bash
./dist/linker-agent -config /tmp/verify-mode-switch.yaml
```

### Step 3: Observe Initial Mode (Terminal 1)

You should see:
```
Mode: strict
```

### Step 4: Switch Mode (Terminal 3)

```bash
/tmp/send-control-command.sh switch_mode '{"mode":"passthrough"}'
```

### Step 5: Verify Mode Changed WITHOUT Restart

**In Terminal 1** (heartbeat receiver), next heartbeat should show:
```
Mode: passthrough
```

**In Terminal 2** (agent logs):
```
switching privacy mode from=strict to=passthrough
```

**Verification:**
- ✅ Agent did NOT restart (uptime continues incrementing)
- ✅ Mode changed immediately
- ✅ Next heartbeat reflects new mode

### Step 6: Switch Back

```bash
/tmp/send-control-command.sh switch_mode '{"mode":"strict"}'
```

**Expected:** Mode changes back to strict without restart

---

## Verification 5: Graceful Shutdown

**What this verifies:** Restart command triggers clean shutdown.

### Step 1: Start Agent

```bash
./dist/linker-agent -config /tmp/verify-control.yaml
```

### Step 2: Send Restart Command

```bash
/tmp/send-control-command.sh restart '{}'
```

**Expected:**
- Response: `"success": true`
- Agent logs show graceful shutdown
- Agent process exits

---

## Verification 6: Integration with Webhook Processing

**What this verifies:** Agent management works alongside webhook processing.

### Step 1: Start Full Stack

**Terminal 1:** Heartbeat receiver
```bash
go run /tmp/heartbeat-receiver.go
```

**Terminal 2:** Agent with all features enabled
```bash
cat > /tmp/verify-full.yaml << 'EOF'
server:
  addr: ":8080"

privacy:
  mode: "strict"
  tenantSalt: "test-salt-verification"

agent:
  organizationId: "org_verify_test"
  version: "1.0.0"

heartbeat:
  enabled: true
  endpoint: "http://localhost:9000/heartbeat"
  interval: 10s
  signatureSecret: "test-heartbeat-secret"

control:
  enabled: true
  listenAddr: ":8081"
  signatureSecret: "test-control-secret-key"

sources:
  stripe:
    apiKey: "${STRIPE_API_KEY}"
    webhook:
      enabled: true
      path: "/webhooks/stripe"
      signingSecret: "${STRIPE_WEBHOOK_SECRET}"

export:
  mode: "file"
  file:
    paths:
      edges: "/tmp/full-verify-edges.jsonl"
      billing: "/tmp/full-verify-billing.jsonl"
      usage: "/tmp/full-verify-usage.jsonl"

matchers:
  recipe:
    name: "default"
    version: "v1"
EOF

# Set real Stripe credentials
export STRIPE_API_KEY="sk_test_..."
export STRIPE_WEBHOOK_SECRET="whsec_..."

./dist/linker-agent -config /tmp/verify-full.yaml
```

### Step 2: Verify All Endpoints Active

```bash
# Webhook endpoint
curl -s http://localhost:8080/healthz

# Control endpoint
curl -s http://localhost:8081/healthz
```

### Step 3: Send Test Webhook

```bash
# Use Stripe CLI or send test webhook event
stripe trigger invoice.payment_succeeded --skip-signature
```

### Step 4: Verify Queue Metrics Update

Watch Terminal 1 for heartbeat with updated queue metrics:
```
Queue: depth=1 dropped=0 enqueued=1 dlq=0
```

### Step 5: Switch Mode While Processing

```bash
# Switch mode during webhook processing
/tmp/send-control-command.sh switch_mode '{"mode":"passthrough"}'
```

**Expected:**
- Mode switches successfully
- Webhook processing continues
- Heartbeats reflect new mode
- No errors or crashes

---

## Summary Checklist

After completing all verifications:

- [ ] Agent generates persistent ID on first run
- [ ] Agent ID survives restarts (same ID after restart)
- [ ] Agent ID format is valid (`agent_<32-hex>`)
- [ ] Heartbeats are sent at configured interval
- [ ] Heartbeat signatures verify correctly
- [ ] Heartbeat includes all required fields
- [ ] Control endpoint responds to health checks
- [ ] Invalid signatures are rejected
- [ ] Valid commands execute successfully
- [ ] Replay protection works (duplicate nonce rejected)
- [ ] Mode switching works without restart
- [ ] Mode change reflected in heartbeats immediately
- [ ] Restart command triggers graceful shutdown
- [ ] Agent management works alongside webhook processing
- [ ] Queue metrics update in heartbeats during processing

---

## Cleanup

```bash
# Stop all running processes (Ctrl+C in each terminal)

# Clean up test files
rm -f /tmp/verify-*.yaml
rm -f /tmp/heartbeat-receiver.go
rm -f /tmp/send-control-command.sh
rm -f /tmp/verify-edges.jsonl
rm -f /tmp/full-verify-*.jsonl
rm -f /var/lib/connektn/agent-id
```

---

## Troubleshooting

**Problem:** Agent ID not persisting

- Check file permissions on `/var/lib/connektn/`
- Verify directory exists and is writable
- Check agent logs for "failed to save agent ID" errors

**Problem:** Heartbeats not arriving

- Verify heartbeat receiver is running
- Check network connectivity to endpoint
- Verify signature secret matches on both sides
- Check agent logs for heartbeat errors

**Problem:** Control commands rejected

- Verify signature calculation matches server-side
- Check clock skew (times must be within 5 minutes)
- Ensure nonce is unique
- Check control secret matches config

**Problem:** Mode doesn't switch

- Verify command response shows success
- Check agent logs for mode switch message
- Ensure mode value is "strict" or "passthrough"
- Wait for next heartbeat to confirm change
