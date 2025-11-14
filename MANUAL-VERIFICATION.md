# Manual Verification Guide (No timeout command required)

This guide provides manual verification steps that work on macOS without GNU `timeout`.

## Quick Overview

For automated verification, use:
```bash
make verify-all              # Runs tests + build
./scripts/verify-agent-id.sh # Tests agent ID persistence
```

For manual step-by-step verification, follow the sections below.

---

## 1. Build Verification

```bash
make build
```

**Expected:**
- ✅ Build completes successfully
- ✅ Binary created at `dist/linker-agent`

---

## 2. Agent ID Persistence (Manual)

### Step 1: Clean Start

```bash
# Remove any existing agent ID
rm -f /var/lib/connektn/agent-id
```

### Step 2: First Run

```bash
# Start the agent (will run webhook server)
./dist/linker-agent -config config.yaml
```

**Watch for in the logs:**
```
Agent ID: agent_<32-hex-characters>
```

**Press Ctrl+C** once you see the Agent ID line.

### Step 3: Verify File Created

```bash
# Check the agent ID was saved
cat /var/lib/connektn/agent-id
```

**Copy this ID** for comparison.

### Step 4: Second Run

```bash
# Start agent again
./dist/linker-agent -config config.yaml
```

**Watch for the same Agent ID** in the logs.

**Press Ctrl+C** after confirming.

### Step 5: Verify Persistence

```bash
# Check agent ID file still has the same value
cat /var/lib/connektn/agent-id
```

**✅ Pass Criteria:**
- Agent ID format: `agent_[0-9a-f]{32}`
- Same ID on both runs
- File persists in `/var/lib/connektn/agent-id`

---

## 3. Heartbeat Transmission (Manual)

### Terminal 1: Start Mock Receiver

```bash
# Start Python heartbeat receiver
python3 << 'EOF'
import json
import hmac
import hashlib
from http.server import HTTPServer, BaseHTTPRequestHandler

SECRET = b"test-heartbeat-secret"

class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers['Content-Length'])
        data = json.loads(self.rfile.read(length))

        # Verify signature
        payload = f"{data['agentId']}:{data['organizationId']}:{data['timestamp']}:{data['uptime']}:{data['mode']}:{data['queueDepth']}:{data['droppedCount']}:{data['enqueuedCount']}:{data['dlqSize']}"
        expected = hmac.new(SECRET, payload.encode(), hashlib.sha256).hexdigest()

        if data['signature'] == expected:
            print(f"✅ Heartbeat: uptime={data['uptime']:3d}s mode={data['mode']}")
        else:
            print(f"❌ Invalid signature")

        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(b'{"status":"ok"}')

    def log_message(self, format, *args):
        pass  # Suppress access logs

print("Mock heartbeat receiver listening on :9000")
print("Press Ctrl+C to stop\n")
HTTPServer(('', 9000), Handler).serve_forever()
EOF
```

### Terminal 2: Configure and Run Agent

```bash
# Create test config with heartbeat enabled
cat > /tmp/verify-heartbeat.yaml << 'EOF'
server:
  addr: ":8080"

privacy:
  mode: "strict"
  tenantSalt: "test-salt"

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
    apiKey: "sk_test_fake"
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

### Expected Results

**In Terminal 1**, you should see heartbeats arriving every 5 seconds:
```
✅ Heartbeat: uptime=  5s mode=strict
✅ Heartbeat: uptime= 10s mode=strict
✅ Heartbeat: uptime= 15s mode=strict
```

**Press Ctrl+C** in both terminals when verified.

**✅ Pass Criteria:**
- Heartbeats arrive at configured interval (5s)
- All heartbeats have ✅ (signature valid)
- Uptime increments correctly
- Mode shows "strict"

---

## 4. Control Commands (Manual)

### Terminal 1: Run Agent with Control Enabled

```bash
cat > /tmp/verify-control.yaml << 'EOF'
server:
  addr: ":8080"

privacy:
  mode: "strict"
  tenantSalt: "test-salt"

agent:
  organizationId: "org_verify_test"
  version: "1.0.0"

heartbeat:
  enabled: false

control:
  enabled: true
  listenAddr: ":8081"
  signatureSecret: "test-control-secret-key"

sources:
  stripe:
    apiKey: "sk_test_fake"
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

### Terminal 2: Test Commands

```bash
# 1. Health check (no signature required)
curl -s http://localhost:8081/healthz | jq .

# 2. Test invalid signature (should be rejected)
curl -s -X POST http://localhost:8081/api/control/command \
  -H "Content-Type: application/json" \
  -d '{
    "command": "restart",
    "timestamp": "'$(date -u +"%Y-%m-%dT%H:%M:%SZ")'",
    "nonce": "'$(uuidgen)'",
    "signature": "invalid-signature"
  }' | jq .

# 3. Build send-command utility
make build-send-command

# 4. Test valid command (switch mode)
./send-command switch_mode mode=passthrough

# 5. Switch back
./send-command switch_mode mode=strict
```

### Expected Results

1. Health check: `{"status":"ok"}`
2. Invalid signature: `"success": false, "message": "...signature verification failed..."`
3. Valid commands: `"success": true, "message": "Command executed successfully"`
4. **In Terminal 1**, agent logs show: `switching privacy mode from=strict to=passthrough`

**✅ Pass Criteria:**
- Health endpoint responds
- Invalid signatures rejected
- Valid commands execute
- Mode changes logged correctly

---

## 5. Mode Switching Without Restart (Manual)

This demonstrates that mode switching happens instantly without restarting the agent.

### Terminal 1: Heartbeat Receiver

```bash
# Start receiver (same as section 3)
make verify-heartbeat-receiver
```

### Terminal 2: Agent with Both Features

```bash
cat > /tmp/verify-both.yaml << 'EOF'
server:
  addr: ":8080"

privacy:
  mode: "strict"
  tenantSalt: "test-salt"

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

sources:
  stripe:
    apiKey: "sk_test_fake"
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

./dist/linker-agent -config /tmp/verify-both.yaml
```

### Terminal 3: Send Mode Switch Command

```bash
# Wait until you see a few heartbeats in Terminal 1 showing "mode=strict"

# Switch mode
./send-command switch_mode mode=passthrough

# Watch Terminal 1 for next heartbeat - should show "mode=passthrough"
# Watch Terminal 2 for log: "switching privacy mode from=strict to=passthrough"
```

### Expected Results

**Terminal 1 (Heartbeat Receiver):**
```
✅ Heartbeat: uptime=  5s mode=strict
✅ Heartbeat: uptime= 10s mode=strict
[mode switch command sent]
✅ Heartbeat: uptime= 15s mode=passthrough   <- Changed!
✅ Heartbeat: uptime= 20s mode=passthrough
```

**Terminal 2 (Agent Logs):**
```
switching privacy mode from=strict to=passthrough
```

**✅ Pass Criteria:**
- **Uptime continues incrementing** (no restart!)
- Mode changes from strict to passthrough
- Change appears in next heartbeat (within 5 seconds)
- Agent keeps running without interruption

---

## Cleanup

After verification:

```bash
# Stop all running agents (Ctrl+C in each terminal)

# Remove test files
rm -f /tmp/verify-*.yaml
rm -f /tmp/verify-edges.jsonl
rm -f /tmp/agent-verify-*.log

# Optional: Remove agent ID to start fresh next time
rm -f /var/lib/connektn/agent-id
```

---

## Summary Checklist

Mark each item after verification:

```
[ ] Build succeeds
[ ] Agent ID created on first run
[ ] Agent ID persists on second run
[ ] Agent ID format valid: agent_[0-9a-f]{32}
[ ] Heartbeats arrive every 5 seconds
[ ] Heartbeat signatures verify (✅)
[ ] Control endpoint responds to health check
[ ] Invalid signatures rejected
[ ] Valid commands execute successfully
[ ] Mode switches from strict to passthrough
[ ] Mode change reflected in heartbeats
[ ] No restart occurs (uptime keeps incrementing)
```

---

## Next Steps

After successful verification:

1. See `PRODUCTION-INTEGRATION.md` for deployment guidance
2. Update `config.yaml` with real Connektn Cloud endpoints
3. Set production environment variables
4. Deploy to Kubernetes with persistent storage
5. Configure monitoring and alerting

---

## Troubleshooting

**Agent ID file not created?**
```bash
# Check directory exists and is writable
sudo mkdir -p /var/lib/connektn
sudo chmod 755 /var/lib/connektn

# Check agent logs for errors
./dist/linker-agent -config config.yaml 2>&1 | tee /tmp/agent.log
```

**Heartbeats not arriving?**
```bash
# Verify receiver is running
lsof -i :9000

# Check agent can reach receiver
curl -X POST http://localhost:9000/heartbeat -d '{}'

# Check agent logs for heartbeat errors
grep -i heartbeat /tmp/agent.log
```

**Control commands rejected?**
```bash
# Verify control server is listening
lsof -i :8081

# Check signature secret matches
grep "signatureSecret" /tmp/verify-control.yaml

# Test with send-command utility (handles signing)
./send-command switch_mode mode=passthrough
```
