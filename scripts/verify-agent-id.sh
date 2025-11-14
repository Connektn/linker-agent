#!/bin/bash

# Verification script for agent ID persistence (macOS compatible)
# Does not require GNU timeout command

set -e

COLOR_GREEN='\033[32m'
COLOR_YELLOW='\033[33m'
COLOR_RESET='\033[0m'

echo -e "${COLOR_GREEN}Testing Agent ID Persistence${COLOR_RESET}"
echo ""

# Clean up any existing agent ID
rm -f /var/lib/connektn/agent-id 2>/dev/null || true

# Ensure agent binary exists
if [ ! -f "./dist/linker-agent" ]; then
    echo -e "${COLOR_YELLOW}Building agent first...${COLOR_RESET}"
    go build -o dist/linker-agent main.go
fi

echo "First run (creating agent ID):"
echo "-------------------------------"

# Start agent in background and capture output
./dist/linker-agent -config config.yaml > /tmp/agent-verify-1.log 2>&1 &
AGENT_PID=$!

# Wait for agent to start and create ID (with timeout)
echo "Waiting for agent to initialize..."
for i in {1..10}; do
    if [ -f /var/lib/connektn/agent-id ]; then
        echo "Agent ID created after ${i} seconds"
        break
    fi
    sleep 1
done

# Extract agent ID from logs
if grep -q "Agent ID" /tmp/agent-verify-1.log; then
    grep "Agent ID" /tmp/agent-verify-1.log
else
    echo "(Checking log file...)"
    tail -20 /tmp/agent-verify-1.log | grep -i "agent" || echo "See /tmp/agent-verify-1.log for details"
fi

# Stop agent
kill $AGENT_PID 2>/dev/null || true
wait $AGENT_PID 2>/dev/null || true
sleep 1

echo ""
echo "Agent ID file contents:"
echo "----------------------"
if [ -f /var/lib/connektn/agent-id ]; then
    FIRST_ID=$(cat /var/lib/connektn/agent-id)
    echo "$FIRST_ID"
else
    echo "❌ Agent ID file was not created!"
    exit 1
fi

echo ""
echo "Second run (should use same ID):"
echo "---------------------------------"

# Start agent again
./dist/linker-agent -config config.yaml > /tmp/agent-verify-2.log 2>&1 &
AGENT_PID=$!

# Wait for agent to start
echo "Waiting for agent to initialize..."
for i in {1..5}; do
    if grep -q "Agent ID" /tmp/agent-verify-2.log; then
        echo "Agent started after ${i} seconds"
        break
    fi
    sleep 1
done

# Extract agent ID from logs
if grep -q "Agent ID" /tmp/agent-verify-2.log; then
    grep "Agent ID" /tmp/agent-verify-2.log
else
    echo "(Checking log file...)"
    tail -20 /tmp/agent-verify-2.log | grep -i "agent" || echo "See /tmp/agent-verify-2.log for details"
fi

# Stop agent
kill $AGENT_PID 2>/dev/null || true
wait $AGENT_PID 2>/dev/null || true
sleep 1

echo ""
echo "Comparing agent IDs:"
echo "-------------------"

SECOND_ID=$(cat /var/lib/connektn/agent-id)
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
rm -f /tmp/agent-verify-1.log /tmp/agent-verify-2.log

echo ""
echo -e "${COLOR_GREEN}✓ Verification complete${COLOR_RESET}"
