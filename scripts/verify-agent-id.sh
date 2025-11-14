#!/bin/bash

# Verification script for agent ID persistence (macOS compatible)
# Does not require GNU timeout command

set -e

COLOR_GREEN='\033[32m'
COLOR_YELLOW='\033[33m'
COLOR_RESET='\033[0m'

echo -e "${COLOR_GREEN}Testing Agent ID Persistence${COLOR_RESET}"
echo ""

# Use home directory (no sudo required)
AGENT_ID_DIR="$HOME/.connektn"
AGENT_ID_FILE="$AGENT_ID_DIR/agent-id"

# Ensure directory exists
mkdir -p "$AGENT_ID_DIR"

# Clean up any existing agent ID
rm -f "$AGENT_ID_FILE" 2>/dev/null || true

# Ensure agent binary exists
if [ ! -f "./dist/linker-agent" ]; then
    echo -e "${COLOR_YELLOW}Building agent first...${COLOR_RESET}"
    go build -o dist/linker-agent main.go
fi

# Create minimal test config
cat > /tmp/verify-agent-config.yaml << 'EOF'
server:
  addr: ":8080"

privacy:
  mode: "strict"
  tenantSalt: "test-verification-salt"

agent:
  organizationId: "org_verify_test"
  version: "1.0.0"

heartbeat:
  enabled: false

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

echo "First run (creating agent ID):"
echo "-------------------------------"

# Start agent in background and capture output (webhook mode for agent management)
./dist/linker-agent -webhook -config /tmp/verify-agent-config.yaml > /tmp/agent-verify-1.log 2>&1 &
AGENT_PID=$!

# Wait for agent to start and create ID (with timeout)
echo "Waiting for agent to initialize..."
for i in {1..10}; do
    if [ -f "$AGENT_ID_FILE" ] && grep -q "Agent ID" /tmp/agent-verify-1.log 2>/dev/null; then
        echo "Agent ID created after ${i} seconds"
        break
    fi
    sleep 1
done

# Give agent a moment to write the file
sleep 1

# Extract agent ID from logs
if grep -q "Agent ID" /tmp/agent-verify-1.log 2>/dev/null; then
    grep "Agent ID" /tmp/agent-verify-1.log
else
    echo "(Checking log file...)"
    if [ -s /tmp/agent-verify-1.log ]; then
        tail -20 /tmp/agent-verify-1.log | grep -i "agent" || echo "See /tmp/agent-verify-1.log for details"
    else
        echo "Log file is empty - agent may not have started"
    fi
fi

# Stop agent
kill $AGENT_PID 2>/dev/null || true
wait $AGENT_PID 2>/dev/null || true
sleep 1

echo ""
echo "Agent ID file contents:"
echo "----------------------"
if [ -f "$AGENT_ID_FILE" ]; then
    FIRST_ID=$(cat "$AGENT_ID_FILE")
    echo "$FIRST_ID"
else
    echo "❌ Agent ID file was not created!"
    exit 1
fi

echo ""
echo "Second run (should use same ID):"
echo "---------------------------------"

# Start agent again (webhook mode)
./dist/linker-agent -webhook -config /tmp/verify-agent-config.yaml > /tmp/agent-verify-2.log 2>&1 &
AGENT_PID=$!

# Wait for agent to start
echo "Waiting for agent to initialize..."
for i in {1..5}; do
    if grep -q "Agent ID" /tmp/agent-verify-2.log 2>/dev/null; then
        echo "Agent started after ${i} seconds"
        break
    fi
    sleep 1
done

# Give agent a moment
sleep 1

# Extract agent ID from logs
if grep -q "Agent ID" /tmp/agent-verify-2.log 2>/dev/null; then
    grep "Agent ID" /tmp/agent-verify-2.log
else
    echo "(Checking log file...)"
    if [ -s /tmp/agent-verify-2.log ]; then
        tail -20 /tmp/agent-verify-2.log | grep -i "agent" || echo "See /tmp/agent-verify-2.log for details"
    else
        echo "Log file is empty - agent may not have started"
    fi
fi

# Stop agent
kill $AGENT_PID 2>/dev/null || true
wait $AGENT_PID 2>/dev/null || true
sleep 1

echo ""
echo "Comparing agent IDs:"
echo "-------------------"

SECOND_ID=$(cat "$AGENT_ID_FILE")
echo "First ID:  $FIRST_ID"
echo "Second ID: $SECOND_ID"

if [ "$FIRST_ID" = "$SECOND_ID" ]; then
    echo ""
    echo -e "${COLOR_GREEN}✅ Agent ID persistence verified!${COLOR_RESET}"
    echo -e "${COLOR_GREEN}   Agent ID survives restarts.${COLOR_RESET}"

    # Validate format
    if [[ $FIRST_ID =~ ^agent_[0-9a-f]{32}$ ]]; then
        echo -e "${COLOR_GREEN}✅ Agent ID format is valid${COLOR_RESET}"
    else
        echo -e "${COLOR_YELLOW}⚠ Agent ID format unexpected: $FIRST_ID${COLOR_RESET}"
        exit 1
    fi
else
    echo ""
    echo "❌ Agent IDs do not match!"
    echo "   Agent ID should persist across restarts."
    exit 1
fi

# Cleanup
rm -f /tmp/agent-verify-1.log /tmp/agent-verify-2.log /tmp/verify-agent-config.yaml /tmp/verify-edges.jsonl

echo ""
echo -e "${COLOR_GREEN}✓ Verification complete${COLOR_RESET}"
