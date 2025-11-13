package main

import (
	"context"
	"encoding/json"
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

func (m *MockQueueMetrics) Depth() int            { return 5 }
func (m *MockQueueMetrics) DLQSize() int          { return 1 }
func (m *MockQueueMetrics) DroppedCount() uint64  { return 10 }
func (m *MockQueueMetrics) EnqueuedCount() uint64 { return 100 }

func main() {
	logger, _ := zap.NewDevelopment()
	defer logger.Sync()

	log.Println("=== Connektn Agent Test Harness ===")
	log.Println()

	// Start mock heartbeat receiver
	go startMockHeartbeatReceiver()
	time.Sleep(500 * time.Millisecond) // Give it time to start

	// Generate or load agent ID
	generator := agentid.NewGenerator("/tmp/test-agent-id")
	agentID, err := generator.GetOrCreate()
	if err != nil {
		log.Fatalf("Failed to get agent ID: %v", err)
	}

	// Create agent
	a, err := agent.New(agent.Config{
		AgentID:        agentID,
		OrganizationID: getEnvOrDefault("ORGANIZATION_ID", "org_test_123"),
		Version:        "1.0.0-test",
		InitialMode:    "strict",
		Logger:         logger,
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	log.Printf("✓ Agent created: %s", a.ID())
	log.Printf("  Organization: %s", a.OrganizationID())
	log.Printf("  Version: %s", a.Version())
	log.Printf("  Mode: %s", a.GetMode())
	log.Println()

	// Create context for shutdown coordination
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create and start heartbeat sender
	heartbeatSecret := getEnvOrDefault("HEARTBEAT_SECRET", "test-heartbeat-secret-key")
	heartbeatSender, err := agent.NewHeartbeatSender(a, agent.HeartbeatSenderConfig{
		Endpoint:     "http://localhost:9000/heartbeat",
		Interval:     10 * time.Second,
		Secret:       heartbeatSecret,
		QueueMetrics: &MockQueueMetrics{},
		Logger:       logger,
	})
	if err != nil {
		log.Fatalf("Failed to create heartbeat sender: %v", err)
	}

	go heartbeatSender.Start(ctx)
	log.Println("✓ Heartbeat sender started (interval: 10s)")
	log.Println()

	// Create control server
	controlSecret := getEnvOrDefault("CONTROL_SECRET", "test-control-secret-key")
	srv := control.NewServer(control.ServerConfig{
		ListenAddr:      ":8081",
		SignatureSecret: controlSecret,
		MaxClockSkew:    5 * time.Minute,
		NonceCacheTTL:   10 * time.Minute,
		Logger:          logger,
	})

	// Register command handlers
	handlers := control.NewHandlers(a, logger)
	// Register each command with the server
	srv.RegisterCommand(control.CommandStop, handlers.HandleStop)
	srv.RegisterCommand(control.CommandStart, handlers.HandleStart)
	srv.RegisterCommand(control.CommandRestart, handlers.HandleRestart)
	srv.RegisterCommand(control.CommandSwitchMode, handlers.HandleSwitchMode)
	srv.RegisterCommand(control.CommandUpgrade, handlers.HandleUpgrade)

	// Set shutdown function
	a.SetShutdownFunc(cancel)

	// Start control server
	go func() {
		if err := srv.Start(); err != nil {
			log.Printf("Control server error: %v", err)
		}
	}()

	log.Println("✓ Control server started")
	log.Println()

	// Print usage information
	log.Println("Test Harness Ready")
	log.Println("==================")
	log.Println()
	log.Println("Endpoints:")
	log.Printf("  • Control: http://localhost:8081/api/control/command")
	log.Printf("  • Health:  http://localhost:8081/healthz")
	log.Printf("  • Mock RX: http://localhost:9000/heartbeat")
	log.Println()
	log.Println("Test Commands:")
	log.Println("  # Health check")
	log.Println("  curl http://localhost:8081/healthz")
	log.Println()
	log.Println("  # Send control command (requires signing)")
	log.Println("  # See docs/TESTING.md for examples")
	log.Println()
	log.Println("Environment Variables:")
	log.Printf("  ORGANIZATION_ID=%s", getEnvOrDefault("ORGANIZATION_ID", "org_test_123 (default)"))
	log.Printf("  CONTROL_SECRET=%s", maskSecret(controlSecret))
	log.Printf("  HEARTBEAT_SECRET=%s", maskSecret(heartbeatSecret))
	log.Println()
	log.Println("Press Ctrl+C to stop")
	log.Println()

	// Wait for signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sigChan:
		log.Println("\n→ Shutdown signal received")
	case <-ctx.Done():
		log.Println("\n→ Agent stopped via control command")
	}

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	log.Println("✓ Shutdown complete")
}

func startMockHeartbeatReceiver() {
	heartbeatSecret := getEnvOrDefault("HEARTBEAT_SECRET", "test-heartbeat-secret-key")

	http.HandleFunc("/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		var payload heartbeat.Payload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			log.Printf("❌ Heartbeat: Failed to decode JSON: %v", err)
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Verify signature
		if !payload.Verify(heartbeatSecret) {
			log.Printf("❌ Heartbeat: Signature verification failed")
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}

		log.Printf("✓ Heartbeat: agent=%s uptime=%ds mode=%s queue=%d dropped=%d",
			payload.AgentID, payload.Uptime, payload.Mode, payload.QueueDepth, payload.DroppedCount)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	log.Println("Starting mock heartbeat receiver on :9000...")
	if err := http.ListenAndServe(":9000", nil); err != nil {
		log.Fatalf("Mock receiver failed: %v", err)
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func maskSecret(secret string) string {
	if len(secret) <= 8 {
		return "***"
	}
	return secret[:4] + "..." + secret[len(secret)-4:]
}
