# Quick Verification Guide - Agent Management Features

This is a streamlined 10-minute verification workflow for the production agent.

## Prerequisites

```bash
make build
```

## 1-Minute Check: Agent ID Persistence

```bash
# Automated script (recommended)
./scripts/verify-agent-id.sh
```

**OR manual verification:**

```bash
# Clean any existing agent ID
rm -f /var/lib/connektn/agent-id

# First run - creates agent ID (will start server, Ctrl+C after you see "Agent ID:")
./dist/linker-agent -config config.yaml

# Check ID was saved
cat /var/lib/connektn/agent-id

# Second run - uses same ID (Ctrl+C after you see "Agent ID:")
./dist/linker-agent -config config.yaml
```

**Expected:** Same agent ID on both runs

---

## 3-Minute Check: Heartbeat Transmission

### Terminal 1: Mock Receiver

```bash
# Quick Python heartbeat receiver
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
            print(f"✅ Heartbeat: uptime={data['uptime']}s mode={data['mode']} queue={data['queueDepth']}")
            self.send_response(200)
        else:
            print(f"❌ Invalid signature")
            self.send_response(401)

        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(b'{"status":"ok"}')

    def log_message(self, format, *args):
        pass  # Suppress access logs

HTTPServer(('', 9000), Handler).serve_forever()
EOF
```

### Terminal 2: Agent with Heartbeat

```bash
cat > /tmp/quick-heartbeat.yaml << 'EOF'
server:
  addr: ":8080"

privacy:
  mode: "strict"
  tenantSalt: "test-salt"

agent:
  organizationId: "org_test"
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
      edges: "/tmp/test-edges.jsonl"

matchers:
  recipe:
    name: "default"
    version: "v1"
EOF

./dist/linker-agent -config /tmp/quick-heartbeat.yaml
```

**Expected:** Terminal 1 shows heartbeats every 5 seconds with ✅

---

## 3-Minute Check: Control Commands

### Terminal 1: Start Agent

```bash
cat > /tmp/quick-control.yaml << 'EOF'
server:
  addr: ":8080"

privacy:
  mode: "strict"
  tenantSalt: "test-salt"

agent:
  organizationId: "org_test"
  version: "1.0.0"

heartbeat:
  enabled: false

control:
  enabled: true
  listenAddr: ":8081"
  signatureSecret: "test-control-secret"

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
      edges: "/tmp/test-edges.jsonl"

matchers:
  recipe:
    name: "default"
    version: "v1"
EOF

./dist/linker-agent -config /tmp/quick-control.yaml
```

### Terminal 2: Test Commands

```bash
# Health check (no auth required)
curl -s http://localhost:8081/healthz | jq .

# Helper function for signed commands
send_command() {
  local cmd="$1"
  local params="${2:-{}}"
  local secret="test-control-secret"

  local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
  local nonce=$(uuidgen 2>/dev/null || cat /proc/sys/kernel/random/uuid)
  local payload="${cmd}${timestamp}${nonce}${params}"
  local signature=$(echo -n "$payload" | openssl dgst -sha256 -hmac "$secret" | awk '{print $2}')

  curl -s -X POST http://localhost:8081/api/control/command \
    -H "Content-Type: application/json" \
    -d "{
      \"command\": \"$cmd\",
      \"params\": $params,
      \"timestamp\": \"$timestamp\",
      \"nonce\": \"$nonce\",
      \"signature\": \"$signature\"
    }" | jq .
}

# Test invalid signature
curl -s -X POST http://localhost:8081/api/control/command \
  -H "Content-Type: application/json" \
  -d '{
    "command": "restart",
    "timestamp": "'$(date -u +"%Y-%m-%dT%H:%M:%SZ")'",
    "nonce": "'$(uuidgen)'",
    "signature": "invalid"
  }' | jq .

# Test valid command
send_command switch_mode '{"mode":"passthrough"}'

# Switch back
send_command switch_mode '{"mode":"strict"}'
```

**Expected:**
- Health: `{"status":"ok"}`
- Invalid sig: `"success": false`
- Valid commands: `"success": true`
- Agent logs show mode switches

---

## 3-Minute Check: Mode Switching Without Restart

This combines heartbeat + control to verify dynamic mode switching.

### Terminal 1: Mock Heartbeat Receiver (from above)

```bash
python3 << 'EOF'
import json, hmac, hashlib
from http.server import HTTPServer, BaseHTTPRequestHandler

SECRET = b"test-heartbeat-secret"

class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        data = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
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

    def log_message(self, *args): pass

HTTPServer(('', 9000), Handler).serve_forever()
EOF
```

### Terminal 2: Agent with Both Enabled

```bash
cat > /tmp/quick-both.yaml << 'EOF'
server:
  addr: ":8080"

privacy:
  mode: "strict"
  tenantSalt: "test-salt"

agent:
  organizationId: "org_test"
  version: "1.0.0"

heartbeat:
  enabled: true
  endpoint: "http://localhost:9000/heartbeat"
  interval: 5s
  signatureSecret: "test-heartbeat-secret"

control:
  enabled: true
  listenAddr: ":8081"
  signatureSecret: "test-control-secret"

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
      edges: "/tmp/test-edges.jsonl"

matchers:
  recipe:
    name: "default"
    version: "v1"
EOF

./dist/linker-agent -config /tmp/quick-both.yaml
```

### Terminal 3: Switch Mode

```bash
# Define helper
send_command() {
  local cmd="$1"
  local params="${2:-{}}"
  local secret="test-control-secret"
  local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
  local nonce=$(uuidgen 2>/dev/null || cat /proc/sys/kernel/random/uuid)
  local payload="${cmd}${timestamp}${nonce}${params}"
  local signature=$(echo -n "$payload" | openssl dgst -sha256 -hmac "$secret" | awk '{print $2}')

  curl -s -X POST http://localhost:8081/api/control/command \
    -H "Content-Type: application/json" \
    -d "{\"command\":\"$cmd\",\"params\":$params,\"timestamp\":\"$timestamp\",\"nonce\":\"$nonce\",\"signature\":\"$signature\"}"
}

# Watch Terminal 1 - you'll see: mode=strict
# Now switch:
send_command switch_mode '{"mode":"passthrough"}' | jq .

# Watch Terminal 1 - next heartbeat shows: mode=passthrough
# Notice uptime keeps incrementing (no restart!)

# Switch back
send_command switch_mode '{"mode":"strict"}' | jq .
```

**Expected in Terminal 1:**
```
✅ Heartbeat: uptime=  5s mode=strict
✅ Heartbeat: uptime= 10s mode=strict
[switch mode command sent]
✅ Heartbeat: uptime= 15s mode=passthrough    <- changed without restart!
✅ Heartbeat: uptime= 20s mode=passthrough
```

**Key observation:** Uptime continues incrementing = no restart occurred

---

## Pass/Fail Checklist

Quick checklist for production readiness:

```bash
# Run these checks, mark ✅ or ❌

# 1. Agent ID persistence
[ ] First run creates new agent ID
[ ] Second run uses same agent ID
[ ] Agent ID format: agent_[0-9a-f]{32}

# 2. Heartbeat functionality
[ ] Heartbeats arrive at configured interval
[ ] Signature verification passes
[ ] All fields present (agentId, uptime, mode, queue metrics)

# 3. Control commands
[ ] Health endpoint responds
[ ] Invalid signatures rejected
[ ] Valid commands execute successfully
[ ] Replay protection works (duplicate nonce fails)

# 4. Mode switching
[ ] Mode switches from strict to passthrough
[ ] Mode switches from passthrough to strict
[ ] No restart occurs (uptime continues)
[ ] Heartbeats reflect new mode immediately

# 5. Production readiness
[ ] Build succeeds: make build
[ ] Tests pass: go test ./...
[ ] Config validates with real secrets
[ ] Documentation complete
```

---

## Quick Troubleshooting

**Heartbeats not arriving?**
```bash
# Check heartbeat receiver is running
lsof -i :9000

# Check agent logs
grep -i heartbeat <agent-log-file>

# Verify network connectivity
curl -X POST http://localhost:9000/heartbeat -d '{}'
```

**Control commands rejected?**
```bash
# Check control server is listening
lsof -i :8081

# Test health endpoint
curl http://localhost:8081/healthz

# Verify signature calculation
echo -n "restart$(date -u +"%Y-%m-%dT%H:%M:%SZ")$(uuidgen){}" | \
  openssl dgst -sha256 -hmac "test-control-secret"
```

**Mode not switching?**
```bash
# Check agent logs for mode switch message
grep -i "switching privacy mode" <agent-log-file>

# Verify command succeeded
send_command switch_mode '{"mode":"passthrough"}' | jq '.success'

# Check current mode in next heartbeat
# (wait up to interval duration)
```

---

## Cleanup

```bash
# Stop all terminals (Ctrl+C)

# Remove test files
rm -f /tmp/quick-*.yaml
rm -f /tmp/test-edges.jsonl

# Optional: remove agent ID to start fresh
rm -f /var/lib/connektn/agent-id
```

---

## Next Steps

After verification passes:

1. **Update config.yaml** with real Connektn Cloud endpoints
2. **Set production secrets** via environment variables
3. **Deploy to Kubernetes** with persistent storage for agent ID
4. **Configure monitoring** based on heartbeat data
5. **Test in staging** before production rollout

See `VERIFICATION.md` for comprehensive testing procedures.
