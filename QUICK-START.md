# Quick Start - Verification

## Run Verification

No setup required! The agent stores its ID in `~/.connektn/agent-id`.

```bash
# Automated agent ID persistence test
./scripts/verify-agent-id.sh
```

**Expected output:**
```
✅ Agent ID persistence verified!
✅ Agent ID format is valid
✓ Verification complete
```

## Manual Verification

If the automated script doesn't work:

### 1. Start Agent (Terminal 1)

```bash
# Clean start
rm -f ~/.connektn/agent-id

# Start agent in webhook mode
./dist/linker-agent -webhook -config config.yaml
```

**Look for:** `Agent ID: agent_<32-hex-characters>`

**Press Ctrl+C** after you see the Agent ID.

### 2. Check Persistence

```bash
# Should show the same ID
cat ~/.connektn/agent-id
```

### 3. Start Agent Again

```bash
# Start again
./dist/linker-agent -webhook -config config.yaml
```

**Verify:** Same Agent ID appears in logs.

## Environment Variables Required

For full testing with heartbeat and control features:

```bash
export HEARTBEAT_SECRET="test-heartbeat-secret"
export CONTROL_SECRET="test-control-secret-key"
export ORGANIZATION_ID="org_test"
```

## See Also

- **MANUAL-VERIFICATION.md** - Detailed step-by-step guide
- **VERIFICATION-SUMMARY.md** - Quick reference card
- **PRODUCTION-INTEGRATION.md** - Deployment guide
