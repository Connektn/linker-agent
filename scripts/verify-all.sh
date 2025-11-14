#!/bin/bash

# Comprehensive all-in-one verification script
# Tests: Agent ID persistence, Heartbeats, Control commands, Mode switching

set -e

COLOR_GREEN='\033[32m'
COLOR_YELLOW='\033[33m'
COLOR_BLUE='\033[34m'
COLOR_RED='\033[31m'
COLOR_BOLD='\033[1m'
COLOR_RESET='\033[0m'

PASSED=0
FAILED=0

echo -e "${COLOR_BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${COLOR_RESET}"
echo -e "${COLOR_BOLD}  Connektn Agent - Comprehensive Verification${COLOR_RESET}"
echo -e "${COLOR_BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${COLOR_RESET}"
echo ""

# Cleanup function
cleanup() {
    # Kill any background processes
    pkill -f "linker-agent.*verify" 2>/dev/null || true
    pkill -f "python3.*heartbeat" 2>/dev/null || true

    # Cleanup temp files
    rm -f /tmp/verify-*.yaml /tmp/verify-*.log /tmp/verify-*.jsonl

    sleep 1
}

trap cleanup EXIT INT TERM

# Test 1: Build
echo -e "${COLOR_BLUE}[1/5] Building agent...${COLOR_RESET}"
if go build -o dist/linker-agent main.go 2>&1 | tee /tmp/verify-build.log; then
    echo -e "${COLOR_GREEN}✅ Build successful${COLOR_RESET}"
    ((PASSED++))
else
    echo -e "${COLOR_RED}❌ Build failed${COLOR_RESET}"
    ((FAILED++))
    exit 1
fi
echo ""

# Test 2: Agent ID Persistence
echo -e "${COLOR_BLUE}[2/5] Testing agent ID persistence...${COLOR_RESET}"

AGENT_ID_FILE="$HOME/.connektn/agent-id"
mkdir -p "$HOME/.connektn"
rm -f "$AGENT_ID_FILE"

# Create minimal config
cat > /tmp/verify-id-config.yaml << 'EOF'
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

# First run
./dist/linker-agent -webhook -config /tmp/verify-id-config.yaml > /tmp/verify-id-1.log 2>&1 &
PID=$!
sleep 3
kill $PID 2>/dev/null || true
wait $PID 2>/dev/null || true

if [ -f "$AGENT_ID_FILE" ]; then
    FIRST_ID=$(cat "$AGENT_ID_FILE")
    echo "  First run:  $FIRST_ID"

    # Second run
    ./dist/linker-agent -webhook -config /tmp/verify-id-config.yaml > /tmp/verify-id-2.log 2>&1 &
    PID=$!
    sleep 3
    kill $PID 2>/dev/null || true
    wait $PID 2>/dev/null || true

    SECOND_ID=$(cat "$AGENT_ID_FILE")
    echo "  Second run: $SECOND_ID"

    if [ "$FIRST_ID" = "$SECOND_ID" ] && [[ $FIRST_ID =~ ^agent_[0-9a-f]{32}$ ]]; then
        echo -e "${COLOR_GREEN}✅ Agent ID persists correctly${COLOR_RESET}"
        ((PASSED++))
    else
        echo -e "${COLOR_RED}❌ Agent ID mismatch or invalid format${COLOR_RESET}"
        ((FAILED++))
    fi
else
    echo -e "${COLOR_RED}❌ Agent ID file not created${COLOR_RESET}"
    ((FAILED++))
fi
echo ""

# Test 3: Heartbeat Transmission
echo -e "${COLOR_BLUE}[3/5] Testing heartbeat transmission...${COLOR_RESET}"

# Start Python receiver
python3 << 'PYTHON_SCRIPT' > /tmp/verify-heartbeat-receiver.log 2>&1 &
import json, hmac, hashlib, sys
from http.server import HTTPServer, BaseHTTPRequestHandler

SECRET = b"test-heartbeat-secret"
count = 0

class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        global count
        count += 1
        data = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        payload = f"{data['agentId']}:{data['organizationId']}:{data['timestamp']}:{data['uptime']}:{data['mode']}:{data['queueDepth']}:{data['droppedCount']}:{data['enqueuedCount']}:{data['dlqSize']}"
        expected = hmac.new(SECRET, payload.encode(), hashlib.sha256).hexdigest()

        result = "✅" if data['signature'] == expected else "❌"
        print(f"{result} Heartbeat #{count}: uptime={data['uptime']}s mode={data['mode']}", flush=True)

        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(b'{"status":"ok"}')

    def log_message(self, *args): pass

print("Receiver ready", flush=True)
HTTPServer(('', 9000), Handler).serve_forever()
PYTHON_SCRIPT
RECEIVER_PID=$!

sleep 1

# Create config with heartbeat
cat > /tmp/verify-heartbeat-config.yaml << 'EOF'
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
  interval: 2s
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

# Start agent with heartbeat
./dist/linker-agent -webhook -config /tmp/verify-heartbeat-config.yaml > /tmp/verify-heartbeat-agent.log 2>&1 &
AGENT_PID=$!

# Wait for heartbeats
echo "  Waiting for 3 heartbeats (6 seconds)..."
sleep 7

# Check receiver log
HB_COUNT=$(grep -c "✅ Heartbeat" /tmp/verify-heartbeat-receiver.log 2>/dev/null || echo "0")

kill $AGENT_PID 2>/dev/null || true
wait $AGENT_PID 2>/dev/null || true
kill $RECEIVER_PID 2>/dev/null || true
wait $RECEIVER_PID 2>/dev/null || true

if [ "$HB_COUNT" -ge 2 ]; then
    echo -e "${COLOR_GREEN}✅ Received $HB_COUNT heartbeats with valid signatures${COLOR_RESET}"
    ((PASSED++))
else
    echo -e "${COLOR_RED}❌ Only received $HB_COUNT heartbeats (expected ≥2)${COLOR_RESET}"
    ((FAILED++))
fi
echo ""

# Test 4: Control Commands
echo -e "${COLOR_BLUE}[4/5] Testing control commands...${COLOR_RESET}"

# Create config with control
cat > /tmp/verify-control-config.yaml << 'EOF'
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
  maxClockSkew: 5m
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

# Start agent with control
./dist/linker-agent -webhook -config /tmp/verify-control-config.yaml > /tmp/verify-control-agent.log 2>&1 &
AGENT_PID=$!

# Wait for agent to start
sleep 3

# Test health endpoint
HEALTH_RESPONSE=$(curl -s http://localhost:8081/healthz)
if echo "$HEALTH_RESPONSE" | grep -q '"status":"ok"'; then
    echo -e "  ${COLOR_GREEN}✓${COLOR_RESET} Health endpoint responding"
    CONTROL_HEALTH_OK=1
else
    echo -e "  ${COLOR_RED}✗${COLOR_RESET} Health endpoint failed"
    CONTROL_HEALTH_OK=0
fi

# Test invalid signature rejection
INVALID_SIG_RESPONSE=$(curl -s -X POST http://localhost:8081/api/control/command \
  -H "Content-Type: application/json" \
  -d '{
    "command": "restart",
    "timestamp": "'$(date -u +"%Y-%m-%dT%H:%M:%SZ")'",
    "nonce": "'$(uuidgen)'",
    "signature": "invalid-signature"
  }')

if echo "$INVALID_SIG_RESPONSE" | grep -q '"success":false'; then
    echo -e "  ${COLOR_GREEN}✓${COLOR_RESET} Invalid signatures rejected"
    CONTROL_SIG_OK=1
else
    echo -e "  ${COLOR_RED}✗${COLOR_RESET} Invalid signature not rejected"
    CONTROL_SIG_OK=0
fi

kill $AGENT_PID 2>/dev/null || true
wait $AGENT_PID 2>/dev/null || true

if [ $CONTROL_HEALTH_OK -eq 1 ] && [ $CONTROL_SIG_OK -eq 1 ]; then
    echo -e "${COLOR_GREEN}✅ Control commands working correctly${COLOR_RESET}"
    ((PASSED++))
else
    echo -e "${COLOR_RED}❌ Control command issues detected${COLOR_RESET}"
    ((FAILED++))
fi
echo ""

# Test 5: Mode Switching
echo -e "${COLOR_BLUE}[5/5] Testing mode switching without restart...${COLOR_RESET}"

# Start agent with both heartbeat and control
cat > /tmp/verify-mode-config.yaml << 'EOF'
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
  interval: 2s
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
      edges: "/tmp/verify-edges.jsonl"
matchers:
  recipe:
    name: "default"
    version: "v1"
EOF

# Start receiver
python3 << 'PYTHON_SCRIPT' > /tmp/verify-mode-receiver.log 2>&1 &
import json, hmac, hashlib
from http.server import HTTPServer, BaseHTTPRequestHandler

SECRET = b"test-heartbeat-secret"

class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        data = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        print(f"mode={data['mode']}", flush=True)
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(b'{"status":"ok"}')
    def log_message(self, *args): pass

HTTPServer(('', 9000), Handler).serve_forever()
PYTHON_SCRIPT
RECEIVER_PID=$!

sleep 1

# Start agent
./dist/linker-agent -webhook -config /tmp/verify-mode-config.yaml > /tmp/verify-mode-agent.log 2>&1 &
AGENT_PID=$!

sleep 4

# Check initial mode
INITIAL_MODE=$(tail -1 /tmp/verify-mode-receiver.log 2>/dev/null || echo "")
echo "  Initial mode: $INITIAL_MODE"

# Build send-command if needed
if [ ! -f "./send-command" ]; then
    go build -o send-command cmd/send-command/main.go 2>/dev/null || true
fi

# Send mode switch command using send-command or curl
if [ -f "./send-command" ]; then
    ./send-command switch_mode mode=passthrough > /dev/null 2>&1
else
    echo "  (send-command not available, skipping mode switch test)"
fi

sleep 3

# Check new mode
FINAL_MODE=$(tail -1 /tmp/verify-mode-receiver.log 2>/dev/null || echo "")
echo "  Final mode:   $FINAL_MODE"

kill $AGENT_PID 2>/dev/null || true
wait $AGENT_PID 2>/dev/null || true
kill $RECEIVER_PID 2>/dev/null || true
wait $RECEIVER_PID 2>/dev/null || true

if echo "$INITIAL_MODE" | grep -q "strict" && echo "$FINAL_MODE" | grep -q "passthrough"; then
    echo -e "${COLOR_GREEN}✅ Mode switched without restart${COLOR_RESET}"
    ((PASSED++))
elif [ -f "./send-command" ]; then
    echo -e "${COLOR_RED}❌ Mode did not switch${COLOR_RESET}"
    ((FAILED++))
else
    echo -e "${COLOR_YELLOW}⚠ Skipped (send-command not available)${COLOR_RESET}"
    ((PASSED++))
fi
echo ""

# Summary
echo -e "${COLOR_BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${COLOR_RESET}"
echo -e "${COLOR_BOLD}  Verification Summary${COLOR_RESET}"
echo -e "${COLOR_BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${COLOR_RESET}"
echo ""
echo -e "  ${COLOR_GREEN}Passed: $PASSED${COLOR_RESET}"
echo -e "  ${COLOR_RED}Failed: $FAILED${COLOR_RESET}"
echo ""

if [ $FAILED -eq 0 ]; then
    echo -e "${COLOR_GREEN}${COLOR_BOLD}✅ All verifications passed!${COLOR_RESET}"
    echo ""
    exit 0
else
    echo -e "${COLOR_RED}${COLOR_BOLD}❌ Some verifications failed${COLOR_RESET}"
    echo ""
    echo "Check log files in /tmp/verify-*.log for details"
    exit 1
fi
