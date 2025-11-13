package control

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestServer_HandleCommand_Success(t *testing.T) {
	secret := "test-secret"
	srv := NewServer(ServerConfig{
		ListenAddr:      ":8081",
		SignatureSecret: secret,
		MaxClockSkew:    5 * time.Minute,
		NonceCacheTTL:   10 * time.Minute,
		Logger:          zap.NewNop(),
	})

	// Register a test command handler
	executed := false
	srv.RegisterCommand(CommandRestart, func(params map[string]interface{}) error {
		executed = true
		return nil
	})

	// Create and sign a command
	cmd := &Command{
		Command:   CommandRestart,
		Timestamp: time.Now().UTC(),
		Nonce:     "test-nonce-success",
	}
	if err := cmd.Sign(secret); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	// Marshal to JSON
	body, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Create request
	req := httptest.NewRequest(http.MethodPost, "/api/control/command", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	// Handle request
	srv.handleCommand(rec, req)

	// Check response
	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Parse result
	var result CommandResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if !result.Success {
		t.Errorf("Result.Success = false, want true: %s", result.Message)
	}

	if !executed {
		t.Error("Command handler was not executed")
	}
}

func TestServer_HandleCommand_InvalidSignature(t *testing.T) {
	secret := "test-secret"
	srv := NewServer(ServerConfig{
		ListenAddr:      ":8081",
		SignatureSecret: secret,
		MaxClockSkew:    5 * time.Minute,
		NonceCacheTTL:   10 * time.Minute,
		Logger:          zap.NewNop(),
	})

	// Register command handler
	srv.RegisterCommand(CommandRestart, func(params map[string]interface{}) error {
		return nil
	})

	// Create command with wrong secret
	cmd := &Command{
		Command:   CommandRestart,
		Timestamp: time.Now().UTC(),
		Nonce:     "test-nonce-invalid-sig",
	}
	if err := cmd.Sign("wrong-secret"); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	// Marshal to JSON
	body, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Create request
	req := httptest.NewRequest(http.MethodPost, "/api/control/command", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	// Handle request
	srv.handleCommand(rec, req)

	// Check response
	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Parse result
	var result CommandResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if result.Success {
		t.Error("Result.Success = true, want false for invalid signature")
	}
}

func TestServer_HandleCommand_ExpiredTimestamp(t *testing.T) {
	secret := "test-secret"
	srv := NewServer(ServerConfig{
		ListenAddr:      ":8081",
		SignatureSecret: secret,
		MaxClockSkew:    5 * time.Minute,
		NonceCacheTTL:   10 * time.Minute,
		Logger:          zap.NewNop(),
	})

	// Register command handler
	srv.RegisterCommand(CommandRestart, func(params map[string]interface{}) error {
		return nil
	})

	// Create command with old timestamp
	cmd := &Command{
		Command:   CommandRestart,
		Timestamp: time.Now().UTC().Add(-10 * time.Minute), // Expired
		Nonce:     "test-nonce-expired",
	}
	if err := cmd.Sign(secret); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	// Marshal to JSON
	body, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Create request
	req := httptest.NewRequest(http.MethodPost, "/api/control/command", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	// Handle request
	srv.handleCommand(rec, req)

	// Check response
	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Parse result
	var result CommandResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	if result.Success {
		t.Error("Result.Success = true, want false for expired timestamp")
	}
}

func TestServer_HandleCommand_ReplayAttack(t *testing.T) {
	secret := "test-secret"
	srv := NewServer(ServerConfig{
		ListenAddr:      ":8081",
		SignatureSecret: secret,
		MaxClockSkew:    5 * time.Minute,
		NonceCacheTTL:   10 * time.Minute,
		Logger:          zap.NewNop(),
	})

	// Register command handler
	executionCount := 0
	srv.RegisterCommand(CommandRestart, func(params map[string]interface{}) error {
		executionCount++
		return nil
	})

	// Create and sign a command
	cmd := &Command{
		Command:   CommandRestart,
		Timestamp: time.Now().UTC(),
		Nonce:     "test-nonce-replay",
	}
	if err := cmd.Sign(secret); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	// Marshal to JSON
	body, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// First request should succeed
	req1 := httptest.NewRequest(http.MethodPost, "/api/control/command", bytes.NewReader(body))
	rec1 := httptest.NewRecorder()
	srv.handleCommand(rec1, req1)

	var result1 CommandResult
	if err := json.Unmarshal(rec1.Body.Bytes(), &result1); err != nil {
		t.Fatalf("Failed to parse result1: %v", err)
	}

	if !result1.Success {
		t.Errorf("First request failed: %s", result1.Message)
	}

	if executionCount != 1 {
		t.Errorf("ExecutionCount after first request = %d, want 1", executionCount)
	}

	// Second request with same nonce should fail (replay)
	req2 := httptest.NewRequest(http.MethodPost, "/api/control/command", bytes.NewReader(body))
	rec2 := httptest.NewRecorder()
	srv.handleCommand(rec2, req2)

	var result2 CommandResult
	if err := json.Unmarshal(rec2.Body.Bytes(), &result2); err != nil {
		t.Fatalf("Failed to parse result2: %v", err)
	}

	if result2.Success {
		t.Error("Second request succeeded (replay attack should be blocked)")
	}

	if executionCount != 1 {
		t.Errorf("ExecutionCount after replay = %d, want 1", executionCount)
	}
}

func TestServer_HandleCommand_MethodNotAllowed(t *testing.T) {
	srv := NewServer(ServerConfig{
		ListenAddr:      ":8081",
		SignatureSecret: "test-secret",
		MaxClockSkew:    5 * time.Minute,
		NonceCacheTTL:   10 * time.Minute,
		Logger:          zap.NewNop(),
	})

	// Try GET request (should be POST only)
	req := httptest.NewRequest(http.MethodGet, "/api/control/command", nil)
	rec := httptest.NewRecorder()

	srv.handleCommand(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestServer_HandleCommand_InvalidJSON(t *testing.T) {
	srv := NewServer(ServerConfig{
		ListenAddr:      ":8081",
		SignatureSecret: "test-secret",
		MaxClockSkew:    5 * time.Minute,
		NonceCacheTTL:   10 * time.Minute,
		Logger:          zap.NewNop(),
	})

	// Send invalid JSON
	req := httptest.NewRequest(http.MethodPost, "/api/control/command", bytes.NewReader([]byte("{invalid json")))
	rec := httptest.NewRecorder()

	srv.handleCommand(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestServer_HandleHealth(t *testing.T) {
	srv := NewServer(ServerConfig{
		ListenAddr:      ":8081",
		SignatureSecret: "test-secret",
		MaxClockSkew:    5 * time.Minute,
		NonceCacheTTL:   10 * time.Minute,
		Logger:          zap.NewNop(),
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	srv.handleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	expected := `{"status":"healthy"}`
	if rec.Body.String() != expected {
		t.Errorf("Body = %q, want %q", rec.Body.String(), expected)
	}
}

func TestServer_StartShutdown(t *testing.T) {
	srv := NewServer(ServerConfig{
		ListenAddr:      ":0", // Random port
		SignatureSecret: "test-secret",
		MaxClockSkew:    5 * time.Minute,
		NonceCacheTTL:   10 * time.Minute,
		Logger:          zap.NewNop(),
	})

	// Start server in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}

	// Wait for Start() to return
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Start returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Start did not return after shutdown")
	}
}
