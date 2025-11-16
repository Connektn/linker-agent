# Linker Agent: Command Polling Implementation

## Overview

Implement command polling functionality in the Linker Agent to receive and execute control commands from the Connektn CDP backend. This enables remote control capabilities such as mode switching, agent restart, and upgrades.

## Endpoint Specification

**Endpoint:** `POST /api/agents/{agentId}/commands/poll`

**Base URL:** Configured CDP backend URL (e.g., `https://api.connektn.io` or `http://localhost:8080`)

**Full URL Pattern:**
```
POST {CDP_BASE_URL}/api/agents/{agentId}/commands/poll
```

## Authentication

Uses HMAC-SHA256 authentication with the **control secret** (NOT the heartbeat secret).

### Required Headers

```
X-Signature: <base64-encoded HMAC signature>
X-Timestamp: <unix timestamp in seconds>
X-Nonce: <unique nonce for this request>
Content-Type: application/json
```

### HMAC Signature Computation

```
message = "{timestamp}|{nonce}|{body}"
signature = Base64(HMAC-SHA256(control_secret, message))
```

**Example:**
```python
import hmac
import hashlib
import base64
import time
import secrets

timestamp = int(time.time())
nonce = base64.b64encode(secrets.token_bytes(16)).decode()
body = '{"organizationId":"org_abc123"}'

message = f"{timestamp}|{nonce}|{body}"
signature = base64.b64encode(
    hmac.new(
        control_secret.encode(),
        message.encode(),
        hashlib.sha256
    ).digest()
).decode()
```

### Request Body

```json
{
  "organizationId": "org_xxx"
}
```

The `organizationId` should be read from the agent's environment variable `ORGANIZATION_ID`.

## Response Format

### When Command Available (200 OK)

```json
{
  "commandId": "uuid-here",
  "command": "switch_mode",
  "params": {
    "mode": "passthrough"
  }
}
```

### When No Commands (204 No Content)

Empty response body.

### Error Responses

**401 Unauthorized** - Invalid HMAC signature or authentication failure
```json
{
  "error": "INVALID_SIGNATURE",
  "message": "HMAC signature does not match"
}
```

**404 Not Found** - Agent ID not found or doesn't belong to organization
```json
{
  "error": "AGENT_NOT_FOUND",
  "message": "Agent not found or doesn't belong to organization"
}
```

## Polling Behavior

### Polling Frequency

- **Normal mode:** Poll every **30 seconds**
- **After command execution:** Poll immediately to check for next command
- **On error:** Exponential backoff (30s, 60s, 120s, max 300s)

### Timing Constraints

- **Request timeout:** 10 seconds
- **Timestamp validation:** Must be within ±300 seconds of server time
- **Nonce uniqueness:** Store recent nonces (last 5 minutes) to prevent replay attacks

## Supported Commands

### 1. switch_mode

Switch agent operating mode.

**Params:**
```json
{
  "mode": "strict" | "passthrough" | "standby"
}
```

**Execution:**
1. Validate mode value
2. Update agent configuration
3. Restart event processing with new mode
4. Acknowledge command with status "success"

**Modes:**
- `strict`: Validate and enforce all event schemas
- `passthrough`: Forward all events without validation
- `standby`: Pause event processing (heartbeat only)

### 2. restart

Gracefully restart the agent.

**Params:**
```json
{}
```

**Execution:**
1. Flush all pending events to CDP
2. Acknowledge command with status "success"
3. Gracefully shutdown and restart process

### 3. upgrade

Upgrade agent to a new version.

**Params:**
```json
{
  "version": "0.2.0",
  "downloadUrl": "https://releases.connektn.io/agent/0.2.0/linker-agent"
}
```

**Execution:**
1. Download new binary from `downloadUrl`
2. Verify checksum (if provided)
3. Replace current binary
4. Acknowledge command with status "success"
5. Restart with new version

## Command Acknowledgment

After receiving a command, the agent must acknowledge it by calling:

**Endpoint:** `POST /api/agents/{agentId}/commands/{commandId}/ack`

**Authentication:** HMAC-SHA256 with control secret (same as poll)

**Request Body:**
```json
{
  "organizationId": "org_xxx",
  "status": "success" | "error",
  "errorMessage": "optional error details"
}
```

**Response:** 204 No Content

## Implementation Checklist

### Phase 1: Basic Polling
- [ ] Add command polling loop (separate goroutine/thread)
- [ ] Implement HMAC authentication for control endpoint
- [ ] Parse and validate command responses
- [ ] Store control secret securely (from environment)
- [ ] Implement polling interval and backoff logic

### Phase 2: Command Execution
- [ ] Implement `switch_mode` command handler
- [ ] Implement `restart` command handler
- [ ] Implement `upgrade` command handler (optional)
- [ ] Add command acknowledgment logic
- [ ] Handle command execution errors gracefully

### Phase 3: Testing & Validation
- [ ] Test HMAC signature validation
- [ ] Test mode switching (strict ↔ passthrough ↔ standby)
- [ ] Test restart command
- [ ] Test error handling and retry logic
- [ ] Verify command acknowledgment

## Environment Variables

Required new environment variables for the agent:

```bash
# Control secret for command polling (different from heartbeat secret)
CONTROL_SECRET=base64-encoded-secret

# Optional: Polling interval override (default: 30)
COMMAND_POLL_INTERVAL_SECONDS=30

# Optional: Enable/disable command polling
COMMAND_POLLING_ENABLED=true
```

## Security Considerations

1. **Separate Secrets:** Use different secrets for heartbeat and control
   - Heartbeat: Read-only, limited damage if compromised
   - Control: Write operations, requires stricter protection

2. **Nonce Validation:** Prevent replay attacks by tracking recent nonces

3. **Timestamp Validation:** Reject requests with stale or future timestamps

4. **Rate Limiting:** Limit polling frequency to prevent API abuse

5. **Command Validation:** Validate all command parameters before execution

## Error Handling

### Network Errors
- Log error and retry with exponential backoff
- Continue heartbeat even if command polling fails
- Alert on persistent failures (>5 minutes)

### Authentication Errors
- Log HMAC validation failures
- Check control secret configuration
- Alert on repeated auth failures (possible credential rotation)

### Command Execution Errors
- Acknowledge command with error status
- Log detailed error message
- Do not retry failed command (backend will handle)

## Logging

Log all command-related activities:

```
[COMMAND] Polling for commands (attempt 1, backoff 30s)
[COMMAND] Received command: switch_mode to passthrough (commandId=abc123)
[COMMAND] Executing command: switch_mode
[COMMAND] Command executed successfully: switch_mode
[COMMAND] Acknowledged command: abc123 (status=success)
[COMMAND] Poll error: network timeout, retrying in 60s
```

## Example Implementation Flow

```
1. Agent starts → Initialize command poller goroutine
2. Every 30s → Poll for commands
3. Command received → Parse and validate
4. Execute command → Update agent state
5. Acknowledge → POST to /ack endpoint
6. Poll immediately → Check for next command
7. No command → Wait 30s, repeat
```

## Testing the Implementation

### Manual Test

1. Start agent with polling enabled
2. Use Dashboard UI to send "Switch Mode to Passthrough" command
3. Check agent logs for command receipt
4. Verify mode switch execution
5. Check Dashboard UI for command status update

### cURL Test

```bash
# Simulate command poll
curl -X POST http://localhost:8080/api/agents/{agentId}/commands/poll \
  -H "X-Signature: {signature}" \
  -H "X-Timestamp: $(date +%s)" \
  -H "X-Nonce: $(openssl rand -base64 16)" \
  -H "Content-Type: application/json" \
  -d '{"organizationId":"org_xxx"}'

# Simulate command acknowledgment
curl -X POST http://localhost:8080/api/agents/{agentId}/commands/{commandId}/ack \
  -H "X-Signature: {signature}" \
  -H "X-Timestamp: $(date +%s)" \
  -H "X-Nonce: $(openssl rand -base64 16)" \
  -H "Content-Type: application/json" \
  -d '{"organizationId":"org_xxx","status":"success"}'
```

## Reference Implementation

See existing heartbeat implementation in the Linker Agent for HMAC authentication patterns. The command polling should follow the same authentication flow but use the `CONTROL_SECRET` instead of `HEARTBEAT_SECRET`.

## Questions?

Contact: Tomas Zezula (tomas@connektn.io)
