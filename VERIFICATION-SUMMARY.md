# Verification Steps - Quick Reference

## Overview

Production agent management features are now integrated into `main.go`. This document provides verification steps to confirm everything works.

---

## Quick Commands

### 1. Build Verification (30 seconds)

```bash
# Build and run tests
make verify-all
```

**Expected:**
- ✅ All tests pass
- ✅ Build succeeds
- ✅ Binary created: `dist/linker-agent`

---

### 2. Agent ID Persistence (1 minute)

```bash
# Automated test
make verify-agent-id
```

**Expected:**
- First run creates new agent ID
- Second run uses same agent ID
- Format: `agent_<32-hex-chars>`

**Manual verification:**
```bash
# Check agent ID file
cat /var/lib/connektn/agent-id
```

---

### 3. Heartbeat Transmission (2 minutes)

**Terminal 1:** Start receiver
```bash
make verify-heartbeat-receiver
```

**Terminal 2:** Run agent with heartbeat config
```bash
# Use config from QUICKSTART-VERIFICATION.md
./dist/linker-agent -config /tmp/quick-heartbeat.yaml
```

**Expected in Terminal 1:**
```
✅ HB: uptime=  5s mode=strict
✅ HB: uptime= 10s mode=strict
✅ HB: uptime= 15s mode=strict
```

---

### 4. Control Commands (2 minutes)

**Terminal 1:** Run agent
```bash
./dist/linker-agent -config /tmp/quick-control.yaml
```

**Terminal 2:** Test commands
```bash
# Health check (no signature required)
curl -s http://localhost:8081/healthz | jq .

# Test invalid signature
curl -s -X POST http://localhost:8081/api/control/command \
  -H "Content-Type: application/json" \
  -d '{"command":"restart","timestamp":"'$(date -u +"%Y-%m-%dT%H:%M:%SZ")'","nonce":"'$(uuidgen)'","signature":"invalid"}' | jq .

# Test valid command (requires send-command utility)
make build-send-command
./send-command switch_mode mode=passthrough
```

**Expected:**
- Health: `{"status":"ok"}`
- Invalid sig: `"success": false`
- Valid command: `"success": true`

---

### 5. Mode Switching Without Restart (3 minutes)

**Terminal 1:** Heartbeat receiver
```bash
make verify-heartbeat-receiver
```

**Terminal 2:** Agent with both features
```bash
./dist/linker-agent -config /tmp/quick-both.yaml
```

**Terminal 3:** Switch mode
```bash
./send-command switch_mode mode=passthrough
```

**Expected:**
- Heartbeats show `mode=strict` initially
- After command: `mode=passthrough`
- **Uptime continues incrementing** (no restart!)
- Mode change is instant

---

## Verification Checklist

Quick pass/fail checklist:

```
[ ] Build succeeds: make verify-all
[ ] Tests pass: go test ./...
[ ] Agent ID created on first run
[ ] Agent ID persists across restarts
[ ] Agent ID format valid: agent_[0-9a-f]{32}
[ ] Heartbeats arrive at interval
[ ] Heartbeat signatures verify
[ ] Control endpoint responds
[ ] Invalid signatures rejected
[ ] Valid commands execute
[ ] Replay protection works
[ ] Mode switches without restart
[ ] Heartbeats reflect mode change
```

---

## Detailed Guides

For comprehensive testing:

- **Manual step-by-step:** `MANUAL-VERIFICATION.md` ⭐ (No timeout command required)
- **10-minute workflow:** `QUICKSTART-VERIFICATION.md`
- **Complete testing:** `VERIFICATION.md`
- **Integration details:** `PRODUCTION-INTEGRATION.md`
- **Documentation:** `README.md` → "Agent Management & Remote Control"

---

## Environment Variables Needed

For full production verification:

```bash
export ORGANIZATION_ID="org_test_123"
export HEARTBEAT_SECRET="test-heartbeat-secret"
export CONTROL_SECRET="test-control-secret"
export STRIPE_API_KEY="sk_test_..."
export STRIPE_WEBHOOK_SECRET="whsec_..."
export TENANT_SALT="your-tenant-salt"
```

---

## Success Criteria

All features verified when:

1. ✅ Build produces working binary
2. ✅ Agent ID persists across restarts
3. ✅ Heartbeats transmit with valid signatures
4. ✅ Control commands execute correctly
5. ✅ Mode switches dynamically without restart
6. ✅ All signatures verify properly
7. ✅ Replay protection prevents duplicate commands
8. ✅ Queue metrics appear in heartbeats

---

## Quick Troubleshooting

**Build fails?**
```bash
go mod tidy
go build -o dist/linker-agent main.go
```

**Agent ID not persisting?**
```bash
# Check directory exists and is writable
mkdir -p /var/lib/connektn
chmod 755 /var/lib/connektn
```

**Heartbeats not arriving?**
```bash
# Check receiver is running
lsof -i :9000

# Check agent logs
grep -i heartbeat <log-file>
```

**Control commands rejected?**
```bash
# Verify control server is listening
lsof -i :8081

# Test health endpoint
curl http://localhost:8081/healthz
```

---

## Next Steps After Verification

Once all checks pass:

1. Update `config.yaml` with production Connektn Cloud endpoints
2. Set production secrets via environment variables
3. Deploy to Kubernetes with persistent storage
4. Configure monitoring based on heartbeat data
5. Test in staging environment
6. Document operational runbook
7. Deploy to production

---

**Documentation Links:**
- Detailed verification: `VERIFICATION.md`
- Quick start: `QUICKSTART-VERIFICATION.md`
- Production setup: `PRODUCTION-INTEGRATION.md`
- Full README: `README.md`
