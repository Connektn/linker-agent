# Testing Guide for Agent Management (Story 2)

This guide shows how to verify the agent management functionality.

## Table of Contents

- [Unit Tests](#unit-tests)
- [Integration Testing](#integration-testing)
- [Manual Testing](#manual-testing)
- [Test Harness](#test-harness)

---

## Unit Tests

All packages have comprehensive unit tests. Run them to verify core functionality:

```bash
# Test all packages
go test ./...

# Test specific packages with verbose output
go test -v ./internal/agentid/...
go test -v ./internal/heartbeat/...
go test -v ./internal/control/...
go test -v ./internal/agent/...

# Test with coverage
go test -cover ./...

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

**Expected output:**
```
ok      linker-agent/internal/agentid    0.XXXs
ok      linker-agent/internal/heartbeat  0.XXXs
ok      linker-agent/internal/control    0.XXXs
ok      linker-agent/internal/agent      0.XXXs
```

---

## Integration Testing

### Prerequisites

You'll need:
1. A running agent (or test harness)
2. Secrets configured
3. `curl` or similar HTTP client

### Step 1: Set Up Test Environment

Create a test configuration file `config-test.yaml`:

```yaml
agent:
  organizationId: "env:ORGANIZATION_ID"
  version: "1.0.0-test"

server:
  addr: ":8080"

heartbeat:
  enabled: true
  interval: 10s
  endpoint: "http://localhost:9000/heartbeat"  # Mock endpoint for testing
  signatureSecret: "env:HEARTBEAT_SECRET"

control:
  enabled: true
  listenAddr: ":8081"
  signatureSecret: "env:CONTROL_SECRET"
  maxClockSkew: 300s
  nonceCache:
    ttl: 600s
    capacity: 1000

privacy:
  mode: "strict"
  idMode: "strict"
  tenantSalt: "env:TENANT_SALT"
  allowPassthroughExports: false

sources:
  stripe:
    apiKey: "env:STRIPE_API_KEY"
    account: ""
    maxRequestsPerSecond: 8
    webhook:
      enabled: false
      path: "/webhooks/stripe"
      signingSecret: "env:STRIPE_WEBHOOK_SECRET"
      maxSkew: 300s
      retry:
        maxAttempts: 5
        baseBackoff: 2s
        maxBackoff: 30s

export:
  mode: "file"
  file:
    paths:
      edges: "/tmp/link_edges.jsonl"
      billing: "/tmp/billing.jsonl"
      usage: "/tmp/usage.jsonl"

matchers:
  recipe:
    name: "default"
    version: "v1"
    weights:
      deterministic_id: 1.0
      temporal_proximity: 0.5
      sku_overlap: 0.8
    threshold: 0.8
    temporalWindowSec: 3600
    skuOverlapMin: 0.5
```

Set environment variables:

```bash
export ORGANIZATION_ID="org_test_123"
export HEARTBEAT_SECRET="test-heartbeat-secret-key"
export CONTROL_SECRET="test-control-secret-key"
export TENANT_SALT="test-tenant-salt-minimum-32-characters-long-required"
export STRIPE_API_KEY="sk_test_dummy_key"
export STRIPE_WEBHOOK_SECRET="whsec_dummy_secret"
```

### Step 2: Build Test Harness

Create a simple test harness to verify the control endpoint:

```bash
# Save as cmd/test-agent/main.go
cat > cmd/test-agent/main.go << 'EOF'
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"linker-agent/internal/agent"
	"linker-agent/internal/agentid"
	"linker-agent/internal/control"

	"go.uber.org/zap"
)

func main() {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// Generate or load agent ID
	generator := agentid.NewGenerator("/tmp/test-agent-id")
	agentID, err := generator.Get()
	if err != nil {
		log.Fatalf("Failed to get agent ID: %v", err)
	}

	log.Printf("Agent ID: %s", agentID)

	// Create agent
	a, err := agent.New(agent.Config{
		AgentID:        agentID,
		OrganizationID: os.Getenv("ORGANIZATION_ID"),
		Version:        "1.0.0-test",
		InitialMode:    "strict",
		Logger:         logger,
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	log.Printf("Agent created: %s (org: %s)", a.ID(), a.OrganizationID())

	// Create control server
	controlSecret := os.Getenv("CONTROL_SECRET")
	if controlSecret == "" {
		log.Fatal("CONTROL_SECRET environment variable is required")
	}

	srv := control.NewServer(control.ServerConfig{
		ListenAddr:      ":8081",
		SignatureSecret: controlSecret,
		MaxClockSkew:    5 * time.Minute,
		NonceCacheTTL:   10 * time.Minute,
		Logger:          logger,
	})

	// Register command handlers
	handlers := control.NewHandlers(a, logger)
	handlers.RegisterAll(srv.Handler)

	// Set up shutdown coordination
	ctx, cancel := context.WithCancel(context.Background())
	a.SetShutdownFunc(cancel)

	// Start control server
	go func() {
		if err := srv.Start(); err != nil {
			log.Printf("Control server error: %v", err)
		}
	}()

	log.Println("Test agent running...")
	log.Println("Control endpoint: http://localhost:8081/api/control/command")
	log.Println("Health check: http://localhost:8081/healthz")

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigChan:
		log.Println("Shutdown signal received")
	case <-ctx.Done():
		log.Println("Agent stopped via control command")
	}

	// Shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Shutdown complete")
}
EOF
```

Build and run:

```bash
# Build test harness
go build -o test-agent cmd/test-agent/main.go

# Run test agent
./test-agent
```

---

## Manual Testing

### Test 1: Health Check

```bash
curl http://localhost:8081/healthz
```

**Expected response:**
```json
{"status":"healthy"}
```

### Test 2: Send Control Command

You'll need to sign commands. Here's a helper script:

```bash
# Save as scripts/send-command.sh
cat > scripts/send-command.sh << 'EOF'
#!/bin/bash
set -e

COMMAND=${1:-restart}
SECRET=${CONTROL_SECRET:-test-control-secret-key}
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
NONCE=$(uuidgen 2>/dev/null || cat /proc/sys/kernel/random/uuid)

# Create unsigned command
UNSIGNED=$(cat <<JSON
{
  "command": "$COMMAND",
  "timestamp": "$TIMESTAMP",
  "nonce": "$NONCE"
}
JSON
)

# Sign command (using OpenSSL)
SIGNATURE=$(echo -n "$UNSIGNED" | openssl dgst -sha256 -hmac "$SECRET" -binary | base64)

# Create signed command
SIGNED=$(echo "$UNSIGNED" | jq --arg sig "$SIGNATURE" '. + {signature: $sig}')

echo "Sending command: $COMMAND"
echo "Request:"
echo "$SIGNED" | jq .

# Send command
curl -s -X POST http://localhost:8081/api/control/command \
  -H "Content-Type: application/json" \
  -d "$SIGNED" | jq .
EOF

chmod +x scripts/send-command.sh
```

**Test restart command:**
```bash
./scripts/send-command.sh restart
```

**Expected response:**
```json
{
  "command": "restart",
  "success": true,
  "message": "Command executed successfully",
  "timestamp": "2025-01-13T12:34:56Z"
}
```

### Test 3: Switch Mode Command

```bash
# Create switch_mode command with parameters
cat > /tmp/switch-mode.json << 'EOF'
{
  "command": "switch_mode",
  "params": {
    "mode": "passthrough"
  },
  "timestamp": "",
  "nonce": ""
}
EOF

# Sign and send (manual steps)
# 1. Add timestamp and nonce
# 2. Sign with HMAC-SHA256
# 3. Add signature
# 4. Send to endpoint
```

Or use a Go signing helper:

```bash
# Save as cmd/sign-command/main.go
cat > cmd/sign-command/main.go << 'EOF'
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"linker-agent/internal/control"

	"github.com/google/uuid"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <command-type> [param=value...]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  %s restart\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s restart mode=graceful\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s switch_mode mode=passthrough\n", os.Args[0])
		os.Exit(1)
	}

	cmdType := control.CommandType(os.Args[1])
	secret := os.Getenv("CONTROL_SECRET")
	if secret == "" {
		fmt.Fprintf(os.Stderr, "CONTROL_SECRET environment variable is required\n")
		os.Exit(1)
	}

	// Parse parameters
	params := make(map[string]interface{})
	for i := 2; i < len(os.Args); i++ {
		// Simple key=value parsing
		// Real implementation would be more robust
		params["mode"] = os.Args[i]
	}

	// Create command
	cmd := &control.Command{
		Command:   cmdType,
		Params:    params,
		Timestamp: time.Now().UTC(),
		Nonce:     uuid.New().String(),
	}

	// Sign
	if err := cmd.Sign(secret); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to sign command: %v\n", err)
		os.Exit(1)
	}

	// Output JSON
	output, err := json.MarshalIndent(cmd, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(output))
}
EOF

# Build and use
go build -o sign-command cmd/sign-command/main.go
./sign-command restart | curl -X POST http://localhost:8081/api/control/command \
  -H "Content-Type: application/json" \
  -d @-
```

### Test 4: Invalid Signature (Should Fail)

```bash
# Send command with wrong signature
curl -X POST http://localhost:8081/api/control/command \
  -H "Content-Type: application/json" \
  -d '{
    "command": "restart",
    "timestamp": "'$(date -u +"%Y-%m-%dT%H:%M:%SZ")'",
    "nonce": "'$(uuidgen)'",
    "signature": "invalid-signature"
  }'
```

**Expected response:**
```json
{
  "command": "restart",
  "success": false,
  "message": "Verification failed: signature verification failed",
  "timestamp": "2025-01-13T12:34:56Z"
}
```

### Test 5: Replay Attack (Should Fail)

```bash
# Send same command twice
SIGNED_CMD=$(./sign-command restart)

# First attempt - should succeed
echo "$SIGNED_CMD" | curl -s -X POST http://localhost:8081/api/control/command \
  -H "Content-Type: application/json" \
  -d @- | jq .

# Second attempt - should fail (replay detected)
echo "$SIGNED_CMD" | curl -s -X POST http://localhost:8081/api/control/command \
  -H "Content-Type: application/json" \
  -d @- | jq .
```

**Expected second response:**
```json
{
  "command": "restart",
  "success": false,
  "message": "Verification failed: nonce already seen (replay attack)",
  "timestamp": "2025-01-13T12:34:56Z"
}
```

---

## Test Harness

For more comprehensive testing, use the included test harness with a mock heartbeat receiver:

```bash
# Save as cmd/test-harness/main.go
cat > cmd/test-harness/main.go << 'EOF'
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"linker-agent/internal/agent"
	"linker-agent/internal/agentid"
	"linker-agent/internal/control"
	"linker-agent/internal/heartbeat"

	"go.uber.org/zap"
)

// MockQueueMetrics provides fake metrics for testing
type MockQueueMetrics struct{}

func (m *MockQueueMetrics) Depth() int           { return 5 }
func (m *MockQueueMetrics) DLQSize() int         { return 1 }
func (m *MockQueueMetrics) DroppedCount() uint64 { return 10 }
func (m *MockQueueMetrics) EnqueuedCount() uint64 { return 100 }

func main() {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	// Start mock heartbeat receiver
	go startMockHeartbeatReceiver()

	// Generate agent ID
	generator := agentid.NewGenerator("/tmp/test-agent-id")
	agentID, err := generator.Get()
	if err != nil {
		log.Fatalf("Failed to get agent ID: %v", err)
	}

	// Create agent
	a, err := agent.New(agent.Config{
		AgentID:        agentID,
		OrganizationID: os.Getenv("ORGANIZATION_ID"),
		Version:        "1.0.0-test",
		InitialMode:    "strict",
		Logger:         logger,
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	log.Printf("✓ Agent created: %s", a.ID())

	// Create and start heartbeat sender
	heartbeatSender, err := agent.NewHeartbeatSender(a, agent.HeartbeatSenderConfig{
		Endpoint:     "http://localhost:9000/heartbeat",
		Interval:     10 * time.Second,
		Secret:       os.Getenv("HEARTBEAT_SECRET"),
		QueueMetrics: &MockQueueMetrics{},
		Logger:       logger,
	})
	if err != nil {
		log.Fatalf("Failed to create heartbeat sender: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go heartbeatSender.Start(ctx)
	log.Println("✓ Heartbeat sender started")

	// Create control server
	srv := control.NewServer(control.ServerConfig{
		ListenAddr:      ":8081",
		SignatureSecret: os.Getenv("CONTROL_SECRET"),
		MaxClockSkew:    5 * time.Minute,
		NonceCacheTTL:   10 * time.Minute,
		Logger:          logger,
	})

	handlers := control.NewHandlers(a, logger)
	handlers.RegisterAll(srv.Handler)

	a.SetShutdownFunc(cancel)

	go func() {
		if err := srv.Start(); err != nil {
			log.Printf("Control server error: %v", err)
		}
	}()

	log.Println("✓ Control server started")
	log.Println()
	log.Println("Test Harness Running")
	log.Println("==================")
	log.Printf("Agent ID: %s", a.ID())
	log.Printf("Control endpoint: http://localhost:8081/api/control/command")
	log.Printf("Health check: http://localhost:8081/healthz")
	log.Printf("Heartbeat receiver: http://localhost:9000/heartbeat")
	log.Println()
	log.Println("Press Ctrl+C to stop")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigChan:
		log.Println("\nShutdown signal received")
	case <-ctx.Done():
		log.Println("\nAgent stopped via control command")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("Shutdown complete")
}

func startMockHeartbeatReceiver() {
	http.HandleFunc("/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		var payload heartbeat.Payload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			log.Printf("❌ Failed to decode heartbeat: %v", err)
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Verify signature
		secret := os.Getenv("HEARTBEAT_SECRET")
		if !payload.Verify(secret) {
			log.Printf("❌ Heartbeat signature verification failed")
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}

		log.Printf("✓ Heartbeat received: agent=%s uptime=%ds queue_depth=%d dropped=%d",
			payload.AgentID, payload.Uptime, payload.QueueDepth, payload.DroppedCount)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	log.Println("Starting mock heartbeat receiver on :9000")
	if err := http.ListenAndServe(":9000", nil); err != nil {
		log.Fatalf("Mock receiver failed: %v", err)
	}
}
EOF

# Build and run
go build -o test-harness cmd/test-harness/main.go
./test-harness
```

---

## Verification Checklist

- [ ] All unit tests pass (`go test ./...`)
- [ ] Health check returns 200 OK
- [ ] Valid control commands succeed
- [ ] Invalid signatures are rejected
- [ ] Replay attacks are blocked
- [ ] Expired timestamps are rejected
- [ ] Heartbeats are sent and verified
- [ ] Privacy mode switching works
- [ ] Agent ID persists across restarts
- [ ] Helm chart lints without errors

---

## Troubleshooting

### "signature verification failed"
- Check that `CONTROL_SECRET` matches on both sides
- Ensure timestamp is in UTC format
- Verify JSON marshaling is consistent (no extra whitespace)

### "nonce already seen (replay attack)"
- This is expected for duplicate commands
- Use a new UUID for each command

### "command timestamp outside acceptable window"
- Check system clocks are synchronized
- Increase `maxClockSkew` if needed

### Heartbeat not sending
- Check `HEARTBEAT_SECRET` is set
- Verify endpoint is reachable
- Check logs for errors

---

## Next Steps

After verifying functionality:
1. Integrate into `main.go` for production use
2. Deploy to Kubernetes and test with Helm
3. Implement cloud-side receiver endpoints
4. Set up monitoring and alerting
