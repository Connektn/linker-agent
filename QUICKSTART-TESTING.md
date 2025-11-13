# Quick Start: Testing Agent Management Features

This guide shows you how to quickly verify the Story 2 implementation (Agent Management & Heartbeats).

## Prerequisites

- Go 1.23+ installed
- Ports 8081 and 9000 available

## 5-Minute Verification

### Step 1: Run All Unit Tests

```bash
make test
```

**Expected:** All tests pass ✅

### Step 2: Start Test Agent

Open a terminal and run:

```bash
make test-agent
```

**Expected output:**
```
=== Connektn Agent Test Harness ===

Starting mock heartbeat receiver on :9000...
✓ Agent created: agent_a1b2c3d4e5f6...
  Organization: org_test_123
  Version: 1.0.0-test
  Mode: strict

✓ Heartbeat sender started (interval: 10s)

✓ Control server started

Test Harness Ready
==================

Endpoints:
  • Control: http://localhost:8081/api/control/command
  • Health:  http://localhost:8081/healthz
  • Mock RX: http://localhost:9000/heartbeat
```

You should see heartbeats arriving every 10 seconds:
```
✓ Heartbeat: agent=agent_... uptime=10s mode=strict queue=5 dropped=10
```

### Step 3: Test Health Check (New Terminal)

Open a **new terminal** and run:

```bash
make test-health
```

**Expected output:**
```json
{
  "status": "healthy"
}
```

### Step 4: Test Control Commands

**Test 1: Send restart command**

```bash
make test-control-restart
```

**Expected output:**
```
Sending command:
{
  "command": "restart",
  "params": {},
  "timestamp": "2025-01-13T...",
  "nonce": "...",
  "signature": "..."
}

Response (HTTP 200):
{
  "command": "restart",
  "success": true,
  "message": "Command executed successfully",
  "timestamp": "2025-01-13T..."
}
```

**Test 2: Switch privacy mode**

```bash
# Switch to passthrough mode
make test-control-switch-mode

# Switch back to strict mode
make test-control-switch-strict
```

**Expected:**
- First command switches from "strict" to "passthrough"
- Second command switches back to "strict"
- Agent logs show: `switching privacy mode from=strict to=passthrough`

**Test 3: Stop agent**

```bash
make test-control-stop
```

**Expected:** Agent shuts down gracefully

## Manual Testing

### Send Custom Commands

Build the command utility:

```bash
make build-send-command
```

Send custom commands:

```bash
# Restart with mode parameter
./send-command restart mode=graceful

# Switch back to strict mode
./send-command switch_mode mode=strict

# Try upgrade (not implemented)
./send-command upgrade version=2.0.0
```

### Test Invalid Scenarios

**Test replay attack:**

```bash
# Send the same command twice (should fail on 2nd attempt)
SIGNED=$(./send-command restart 2>&1 | grep -A 20 "Sending command" | tail -n +2)
echo "$SIGNED" | curl -X POST http://localhost:8081/api/control/command \
  -H "Content-Type: application/json" -d @-

# Wait a moment, then send again (should be rejected)
echo "$SIGNED" | curl -X POST http://localhost:8081/api/control/command \
  -H "Content-Type: application/json" -d @-
```

**Test invalid signature:**

```bash
curl -X POST http://localhost:8081/api/control/command \
  -H "Content-Type: application/json" \
  -d '{
    "command": "restart",
    "timestamp": "'$(date -u +"%Y-%m-%dT%H:%M:%SZ")'",
    "nonce": "test",
    "signature": "invalid"
  }'
```

**Expected:** Both should return `"success": false`

## Verification Checklist

- [x] All unit tests pass
- [x] Test agent starts successfully
- [x] Agent ID is generated and persisted
- [x] Heartbeats are sent every 10 seconds
- [x] Heartbeat signatures are valid
- [x] Health check returns 200 OK
- [x] Valid control commands execute successfully
- [x] Invalid signatures are rejected
- [x] Replay attacks are blocked
- [x] Privacy mode switching works
- [x] Agent stops gracefully on stop command

## What's Happening

When you run `make test-agent`, it:

1. **Starts mock heartbeat receiver** (port 9000)
   - Receives and verifies heartbeat signatures
   - Logs each heartbeat with metrics

2. **Generates agent ID** (persisted at `/tmp/test-agent-id`)
   - Format: `agent_{32-char-hex}`
   - Reuses same ID on restart

3. **Starts control server** (port 8081)
   - Listens for signed commands
   - Verifies signatures with HMAC-SHA256
   - Tracks nonces to prevent replay

4. **Sends periodic heartbeats**
   - Every 10 seconds
   - Includes queue metrics
   - Signed with HMAC-SHA256

## Next Steps

- See `docs/TESTING.md` for comprehensive testing guide
- See `docs/API.md` for full API documentation
- Ready to integrate into main.go for production use

## Cleanup

Stop the test agent with `Ctrl+C` and clean up:

```bash
make clean
rm -f /tmp/test-agent-id
```
