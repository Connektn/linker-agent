# Production Integration Complete - Agent Management Features

## Summary

Agent management and heartbeat features (Story 2) have been successfully integrated into the **production webhook server mode** in `main.go`. The agent now supports remote control, heartbeat monitoring, and dynamic privacy mode switching without restarts.

## What Was Integrated

### 1. Production Main Entry Point (`main.go`)

The `runWebhookServer()` function now includes full agent management:

- **Agent initialization**: Persistent agent ID generation/loading
- **Heartbeat sender**: Periodic health metrics transmission (if enabled)
- **Control server**: Remote command endpoint (if enabled)
- **Graceful shutdown**: Coordinated shutdown of all components

### 2. Queue Metrics Interface

The Exporter now implements `QueueMetricsProvider` interface:

```go
// Implemented in internal/exporter/exporter.go
func (e *Exporter) Depth() int
func (e *Exporter) DLQSize() int
func (e *Exporter) DroppedCount() uint64
func (e *Exporter) EnqueuedCount() uint64
```

These methods aggregate metrics across all stream queues (edges, billing, usage) for heartbeat reporting.

### 3. Configuration

Updated `config.yaml` with agent management sections:

```yaml
agent:
  organizationId: "env:ORGANIZATION_ID"
  version: "1.0.0"

heartbeat:
  enabled: true
  endpoint: "https://api.connektn.io/agent/heartbeat"
  interval: 30s
  signatureSecret: "env:HEARTBEAT_SECRET"

control:
  enabled: true
  listenAddr: ":8081"
  signatureSecret: "env:CONTROL_SECRET"
  maxClockSkew: 5m
  nonceCache:
    ttl: 10m
```

### 4. Documentation

Added comprehensive "Agent Management & Remote Control" section to README.md covering:

- Heartbeat monitoring
- Control commands (switch_mode, restart, stop, start, upgrade)
- Configuration examples
- Security model
- Persistent agent ID

## How to Use

### Environment Variables

```bash
export ORGANIZATION_ID="your-org-id"
export HEARTBEAT_SECRET="your-heartbeat-secret"
export CONTROL_SECRET="your-control-secret"
export STRIPE_API_KEY="sk_test_..."
export STRIPE_WEBHOOK_SECRET="whsec_..."
export TENANT_SALT="your-tenant-salt"
```

### Run Production Agent

```bash
# Build
make build

# Run with webhook mode (agent management enabled)
./dist/linker-agent -config config.yaml
```

The agent will:
1. Generate/load persistent agent ID from `/var/lib/connektn/agent-id`
2. Start Stripe webhook server on `:8080`
3. Start heartbeat sender (sends to cloud every 30s)
4. Start control command server on `:8081`
5. Process webhooks and send anonymized link graphs to configured export sink

### Control Commands

**Switch privacy mode (no restart required):**

```bash
curl -X POST http://localhost:8081/api/control/command \
  -H "Content-Type: application/json" \
  -d '{
    "command": "switch_mode",
    "params": {"mode": "passthrough"},
    "timestamp": "'$(date -u +"%Y-%m-%dT%H:%M:%SZ")'",
    "nonce": "'$(uuidgen)'",
    "signature": "<hmac-signature>"
  }'
```

**Graceful restart:**

```bash
curl -X POST http://localhost:8081/api/control/command \
  -H "Content-Type: application/json" \
  -d '{
    "command": "restart",
    "timestamp": "'$(date -u +"%Y-%m-%dT%H:%M:%SZ")'",
    "nonce": "'$(uuidgen)'",
    "signature": "<hmac-signature>"
  }'
```

## Production Deployment

### Kubernetes

The agent is designed for Kubernetes deployment:

1. **Persistent agent ID**: Use a PersistentVolumeClaim for `/var/lib/connektn/agent-id`
2. **Secrets**: Store signing secrets in Kubernetes Secrets
3. **Network policies**: Restrict access to control endpoint (`:8081`)
4. **Auto-restart**: Pod restart policy ensures automatic recovery after `restart` command
5. **Zero downtime mode switching**: `switch_mode` changes privacy mode without pod restart

### Security Considerations

- **Heartbeat endpoint**: Should point to Connektn Cloud (`https://api.connektn.io/agent/heartbeat`)
- **Control endpoint**: Should be behind firewall/VPN, not publicly exposed
- **Signature secrets**: Rotate periodically, never commit to version control
- **TLS**: Use mutual TLS for control endpoint in high-security environments

## Features Enabled

✅ **Persistent agent identity** - Survives restarts
✅ **Heartbeat transmission** - Health monitoring with HMAC signatures
✅ **Remote control** - Dynamic configuration changes
✅ **Zero-downtime mode switching** - No restart required
✅ **Graceful shutdown** - Coordinated cleanup
✅ **Queue metrics reporting** - Real-time export queue health
✅ **Replay protection** - Nonce-based command deduplication
✅ **Clock skew tolerance** - Handles time sync issues

## Testing

All tests pass:

```bash
$ go test ./...
ok  	linker-agent/internal/agent	(cached)
ok  	linker-agent/internal/agentid	(cached)
ok  	linker-agent/internal/config	(cached)
ok  	linker-agent/internal/control	(cached)
ok  	linker-agent/internal/exporter	1.075s
ok  	linker-agent/internal/heartbeat	(cached)
```

Production build successful:

```bash
$ go build -o dist/linker-agent main.go
$ ls -lh dist/linker-agent
-rwxr-xr-x  1 tom  staff   12M Nov 14 00:02 dist/linker-agent
```

## Files Modified

1. `main.go` - Integrated agent management into `runWebhookServer()`
2. `internal/exporter/exporter.go` - Added `QueueMetricsProvider` interface methods
3. `internal/exporter/queue.go` - Added `dlqSize()` method
4. `config.yaml` - Added agent/heartbeat/control sections
5. `README.md` - Added "Agent Management & Remote Control" documentation

## Next Steps

For real customer deployments:

1. Configure Connektn Cloud heartbeat endpoint
2. Set up Kubernetes deployment with persistent storage
3. Configure network policies for control endpoint
4. Set up monitoring/alerting based on heartbeat status
5. Test mode switching in staging environment
6. Document runbook for operations team

## Notes

- The test harness (`cmd/test-agent`) remains available for local testing
- Production mode uses real webhook processing + agent management
- All features are production-ready and tested
- Configuration is fully declarative via YAML + environment variables
