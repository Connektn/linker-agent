#!/bin/bash

# Comprehensive heartbeat verification script
# Starts mock receiver, agent, and displays heartbeats in real-time

set -e

COLOR_GREEN='\033[32m'
COLOR_YELLOW='\033[33m'
COLOR_BLUE='\033[34m'
COLOR_RESET='\033[0m'

echo -e "${COLOR_GREEN}Heartbeat Verification${COLOR_RESET}"
echo ""

# Cleanup function
cleanup() {
    echo ""
    echo -e "${COLOR_YELLOW}Cleaning up...${COLOR_RESET}"

    # Kill agent if running
    if [ ! -z "$AGENT_PID" ]; then
        kill $AGENT_PID 2>/dev/null || true
        wait $AGENT_PID 2>/dev/null || true
    fi

    # Kill receiver if running
    if [ ! -z "$RECEIVER_PID" ]; then
        kill $RECEIVER_PID 2>/dev/null || true
        wait $RECEIVER_PID 2>/dev/null || true
    fi

    # Cleanup temp files
    rm -f /tmp/verify-heartbeat-*.yaml /tmp/verify-heartbeat-*.log /tmp/verify-edges.jsonl

    echo -e "${COLOR_GREEN}✓ Cleanup complete${COLOR_RESET}"
}

# Set trap to cleanup on exit
trap cleanup EXIT INT TERM

# Check if Python 3 is available
if ! command -v python3 &> /dev/null; then
    echo -e "${COLOR_YELLOW}⚠ Python 3 not found. Using alternative receiver.${COLOR_RESET}"
    USE_GO_RECEIVER=1
else
    USE_GO_RECEIVER=0
fi

# Ensure agent binary exists
if [ ! -f "./dist/linker-agent" ]; then
    echo -e "${COLOR_YELLOW}Building agent...${COLOR_RESET}"
    go build -o dist/linker-agent main.go
fi

# Create test config
cat > /tmp/verify-heartbeat-config.yaml << 'EOF'
server:
  addr: ":8080"

privacy:
  mode: "strict"
  tenantSalt: "test-verification-salt"

agent:
  organizationId: "org_verify_test"
  version: "1.0.0"

heartbeat:
  enabled: true
  endpoint: "http://localhost:9000/heartbeat"
  interval: 3s
  signatureSecret: "test-heartbeat-secret"

control:
  enabled: false

sources:
  stripe:
    apiKey: "sk_test_fake_verification"
    webhook:
      enabled: true
      path: "/webhooks/stripe"
      signingSecret: "whsec_fake_verification"

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

echo -e "${COLOR_BLUE}Step 1: Starting mock heartbeat receiver on port 9000${COLOR_RESET}"

# Start Python receiver
if [ $USE_GO_RECEIVER -eq 0 ]; then
    python3 << 'PYTHON_SCRIPT' > /tmp/verify-heartbeat-receiver.log 2>&1 &
import json
import hmac
import hashlib
from http.server import HTTPServer, BaseHTTPRequestHandler

SECRET = b"test-heartbeat-secret"
count = 0

class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        global count
        count += 1

        length = int(self.headers['Content-Length'])
        data = json.loads(self.rfile.read(length))

        # Verify signature
        payload = f"{data['agentId']}:{data['organizationId']}:{data['timestamp']}:{data['uptime']}:{data['mode']}:{data['queueDepth']}:{data['droppedCount']}:{data['enqueuedCount']}:{data['dlqSize']}"
        expected = hmac.new(SECRET, payload.encode(), hashlib.sha256).hexdigest()

        if data['signature'] == expected:
            print(f"[{count:2d}] ✅ Heartbeat: uptime={data['uptime']:3d}s mode={data['mode']:12s} queue={data['queueDepth']} dropped={data['droppedCount']}")
        else:
            print(f"[{count:2d}] ❌ Invalid signature")

        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(b'{"status":"ok"}')

    def log_message(self, format, *args):
        pass  # Suppress HTTP access logs

print("Mock heartbeat receiver started on :9000")
print("Waiting for heartbeats...\n")
HTTPServer(('', 9000), Handler).serve_forever()
PYTHON_SCRIPT
    RECEIVER_PID=$!
else
    echo -e "${COLOR_YELLOW}Python receiver not available - heartbeats will be logged to file${COLOR_RESET}"
fi

# Wait for receiver to start
sleep 1

# Check if receiver is listening
if ! lsof -i :9000 > /dev/null 2>&1; then
    echo -e "${COLOR_YELLOW}⚠ Warning: Port 9000 not listening. Heartbeats may not be received.${COLOR_RESET}"
fi

echo -e "${COLOR_BLUE}Step 2: Starting agent with heartbeat enabled (interval: 3s)${COLOR_RESET}"

# Start agent in background
./dist/linker-agent -webhook -config /tmp/verify-heartbeat-config.yaml > /tmp/verify-heartbeat-agent.log 2>&1 &
AGENT_PID=$!

# Wait for agent to initialize
echo "Waiting for agent to start..."
for i in {1..10}; do
    if grep -q "Agent ID" /tmp/verify-heartbeat-agent.log 2>/dev/null; then
        AGENT_ID=$(grep "Agent ID" /tmp/verify-heartbeat-agent.log | head -1 | awk '{print $NF}')
        echo -e "${COLOR_GREEN}✓ Agent started: $AGENT_ID${COLOR_RESET}"
        break
    fi
    sleep 1
done

echo ""
echo -e "${COLOR_BLUE}Step 3: Watching heartbeats (Ctrl+C to stop)${COLOR_RESET}"
echo -e "${COLOR_YELLOW}Expected: Heartbeat every 3 seconds with incrementing uptime${COLOR_RESET}"
echo ""

# Tail the receiver log to show heartbeats in real-time
if [ $USE_GO_RECEIVER -eq 0 ]; then
    # Show heartbeats from Python receiver
    tail -f /tmp/verify-heartbeat-receiver.log 2>/dev/null || {
        echo -e "${COLOR_YELLOW}Heartbeat receiver not outputting. Check agent logs:${COLOR_RESET}"
        tail -f /tmp/verify-heartbeat-agent.log | grep -i heartbeat || true
    }
else
    # Show heartbeats from agent log
    tail -f /tmp/verify-heartbeat-agent.log | grep -i "heartbeat" || true
fi
