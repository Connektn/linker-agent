# Linker Agent API Documentation

This document describes the HTTP APIs exposed by the Connektn Linker Agent for management and control.

## Table of Contents

- [Control Command API](#control-command-api)
- [Heartbeat API](#heartbeat-api)
- [Security](#security)

---

## Control Command API

The Control Command API allows Connektn Cloud to send authenticated commands to the agent for lifecycle management and configuration changes.

### Endpoint

```
POST /api/control/command
```

### Request

**Headers:**
- `Content-Type: application/json`

**Body:**
```json
{
  "command": "restart",
  "params": {
    "mode": "graceful"
  },
  "timestamp": "2025-01-13T12:00:00Z",
  "nonce": "unique-nonce-123",
  "signature": "base64-encoded-hmac-sha256-signature"
}
```

**Fields:**
- `command` (string, required): Command type. One of:
  - `stop`: Gracefully shut down the agent
  - `start`: Start the agent (placeholder for future use)
  - `restart`: Restart the agent
  - `switch_mode`: Switch privacy mode
  - `upgrade`: Upgrade agent to new version (not yet implemented)
- `params` (object, optional): Command-specific parameters
- `timestamp` (string, required): ISO 8601 timestamp in UTC
- `nonce` (string, required): Unique identifier to prevent replay attacks
- `signature` (string, required): HMAC-SHA256 signature (base64-encoded)

### Response

**Success (200 OK):**
```json
{
  "command": "restart",
  "success": true,
  "message": "Command executed successfully",
  "timestamp": "2025-01-13T12:00:01Z"
}
```

**Failure (200 OK with success=false):**
```json
{
  "command": "restart",
  "success": false,
  "message": "Verification failed: signature verification failed",
  "timestamp": "2025-01-13T12:00:01Z"
}
```

### Command-Specific Parameters

#### `stop` Command
No parameters required.

```json
{
  "command": "stop",
  "timestamp": "2025-01-13T12:00:00Z",
  "nonce": "nonce-123",
  "signature": "..."
}
```

#### `restart` Command
Optional parameters:
- `mode` (string): "graceful" (default) or "immediate"

```json
{
  "command": "restart",
  "params": {
    "mode": "graceful"
  },
  "timestamp": "2025-01-13T12:00:00Z",
  "nonce": "nonce-123",
  "signature": "..."
}
```

#### `switch_mode` Command
Required parameters:
- `mode` (string): "strict" or "passthrough"

```json
{
  "command": "switch_mode",
  "params": {
    "mode": "passthrough"
  },
  "timestamp": "2025-01-13T12:00:00Z",
  "nonce": "nonce-123",
  "signature": "..."
}
```

#### `upgrade` Command
Required parameters:
- `version` (string): Target version
- `url` (string, optional): Custom download URL

```json
{
  "command": "upgrade",
  "params": {
    "version": "2.0.0",
    "url": "https://releases.connektn.io/agent/2.0.0"
  },
  "timestamp": "2025-01-13T12:00:00Z",
  "nonce": "nonce-123",
  "signature": "..."
}
```

### Security

See [Security](#security) section below for signature generation details.

### Configuration

Control server is configured via YAML:

```yaml
control:
  enabled: true
  listenAddr: ":8081"
  signatureSecret: "env:CONTROL_SECRET"
  maxClockSkew: 300s  # 5 minutes
  nonceCache:
    ttl: 600s  # 10 minutes
    capacity: 1000
```

---

## Heartbeat API

The Heartbeat API is used by the agent to send periodic health and status updates to Connektn Cloud.

### Endpoint

```
POST https://api.connektn.io/agent/heartbeat
```

(Cloud-side endpoint, called by agent)

### Request

**Headers:**
- `Content-Type: application/json`

**Body:**
```json
{
  "agent_id": "agent_a1b2c3d4e5f6...",
  "organization_id": "org_123456",
  "version": "1.0.0",
  "uptime_seconds": 3600,
  "mode": "strict",
  "queue_depth": 10,
  "dlq_size": 2,
  "dropped_count": 5,
  "retry_count": 3,
  "last_flush_ts": "2025-01-13T11:55:00Z",
  "timestamp": "2025-01-13T12:00:00Z",
  "signature": "base64-encoded-hmac-sha256-signature"
}
```

**Fields:**
- `agent_id` (string): Agent identifier (format: `agent_{32-char-hex}`)
- `organization_id` (string): Organization identifier
- `version` (string): Agent version
- `uptime_seconds` (integer): Seconds since agent started
- `mode` (string): Privacy mode ("strict" or "passthrough")
- `queue_depth` (integer): Current queue depth
- `dlq_size` (integer): Dead letter queue size
- `dropped_count` (integer): Total items dropped
- `retry_count` (integer): Total retry attempts
- `last_flush_ts` (string): ISO 8601 timestamp of last successful export
- `timestamp` (string): ISO 8601 timestamp of heartbeat generation
- `signature` (string): HMAC-SHA256 signature (base64-encoded)

### Response

**Success (200 OK):**
```json
{
  "status": "ok"
}
```

### Configuration

Heartbeat is configured via YAML:

```yaml
heartbeat:
  enabled: true
  interval: 30s
  endpoint: "https://api.connektn.io/agent/heartbeat"
  signatureSecret: "env:HEARTBEAT_SECRET"
```

---

## Security

Both the Control Command API and Heartbeat API use HMAC-SHA256 signatures to ensure message authenticity and prevent tampering.

### Signature Generation

1. Create a copy of the message without the `signature` field
2. Marshal to JSON (compact, no extra whitespace)
3. Compute HMAC-SHA256 using the shared secret
4. Encode result as base64
5. Set the `signature` field to the base64-encoded value

**Example (pseudo-code):**
```go
// Remove signature field
unsigned := message
unsigned.Signature = ""

// Marshal to JSON
data := json.Marshal(unsigned)

// Compute HMAC
mac := hmac.New(sha256.New, []byte(secret))
mac.Write(data)
signature := mac.Sum(nil)

// Encode as base64
message.Signature = base64.StdEncoding.EncodeToString(signature)
```

### Signature Verification

The receiver performs the same steps and uses constant-time comparison to verify:

```go
hmac.Equal(receivedSignature, computedSignature)
```

### Replay Protection (Control Commands)

Control commands include three layers of protection:

1. **Signature Verification**: Ensures message authenticity
2. **Timestamp Validation**: Rejects commands outside clock skew window (default: 5 minutes)
3. **Nonce Tracking**: Prevents replay of valid commands

**Nonce Cache:**
- Stores recently seen nonces
- TTL: 10 minutes (default)
- Capacity: 1000 nonces (default)
- Cleanup: Automatic on each verification

### Secret Management

Secrets are provided via environment variables:

**Control Command Secret:**
```bash
export CONTROL_SECRET="your-secret-key"
```

**Heartbeat Secret:**
```bash
export HEARTBEAT_SECRET="your-secret-key"
```

In Kubernetes, use Secrets:
```yaml
env:
  - name: CONTROL_SECRET
    valueFrom:
      secretKeyRef:
        name: connektn-agent-secrets
        key: CONTROL_SECRET
```

### Threat Model

**Assumptions:**
- Secrets are securely exchanged during agent onboarding
- Secrets are stored securely (Kubernetes Secrets, vault, etc.)
- Network transport uses HTTPS (TLS)

**Protections:**
- HMAC-SHA256 prevents unauthorized commands
- Nonce cache prevents replay attacks
- Timestamp validation prevents delayed replay
- Constant-time comparison prevents timing attacks

**Out of Scope:**
- Secret rotation (manual process)
- Rate limiting (handled by Kubernetes ingress/API gateway)
- DDoS protection (handled by infrastructure layer)

---

## Health Check Endpoints

The agent exposes standard Kubernetes health check endpoints:

### Liveness Probe

```
GET /healthz
```

Returns `200 OK` if the agent process is running.

### Readiness Probe

```
GET /readyz
```

Returns `200 OK` if the agent is ready to process requests.

---

## Examples

### Sending a Control Command (Cloud → Agent)

```bash
# Generate command
COMMAND='{
  "command": "restart",
  "params": {"mode": "graceful"},
  "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'",
  "nonce": "'$(uuidgen)'"
}'

# Sign command (implementation depends on language)
SIGNATURE=$(echo -n "$COMMAND" | openssl dgst -sha256 -hmac "$SECRET" -binary | base64)

# Add signature
SIGNED_COMMAND=$(echo "$COMMAND" | jq --arg sig "$SIGNATURE" '. + {signature: $sig}')

# Send command
curl -X POST http://agent:8081/api/control/command \
  -H "Content-Type: application/json" \
  -d "$SIGNED_COMMAND"
```

### Agent Sending Heartbeat (Agent → Cloud)

Handled automatically by the agent's HeartbeatSender component.

Configuration:
```yaml
heartbeat:
  enabled: true
  interval: 30s
  endpoint: "https://api.connektn.io/agent/heartbeat"
  signatureSecret: "env:HEARTBEAT_SECRET"
```

---

## Version History

- **v1.0.0** (2025-01-13): Initial API specification
  - Control Command API
  - Heartbeat API
  - HMAC-SHA256 signatures
  - Replay protection
