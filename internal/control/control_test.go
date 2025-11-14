package control

import (
	"testing"
	"time"
)

func TestCommand_Sign(t *testing.T) {
	cmd := &Command{
		Command:   CommandRestart,
		Params:    map[string]interface{}{"mode": "graceful"},
		Timestamp: time.Now().UTC(),
		Nonce:     "test-nonce-123",
	}

	secret := "test-secret"

	if err := cmd.Sign(secret); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	if cmd.Signature == "" {
		t.Error("Signature is empty after signing")
	}
}

func TestCommand_VerifySignature(t *testing.T) {
	cmd := &Command{
		Command:   CommandRestart,
		Params:    map[string]interface{}{"mode": "graceful"},
		Timestamp: time.Now().UTC(),
		Nonce:     "test-nonce-123",
	}

	secret := "test-secret"

	// Sign the command
	if err := cmd.Sign(secret); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	// Verify with correct secret
	if !cmd.verifySignature(secret) {
		t.Error("verifySignature failed with correct secret")
	}

	// Verify with wrong secret
	if cmd.verifySignature("wrong-secret") {
		t.Error("verifySignature succeeded with wrong secret (should fail)")
	}
}

func TestCommand_Verify_Tampering(t *testing.T) {
	cmd := &Command{
		Command:   CommandRestart,
		Params:    map[string]interface{}{"mode": "graceful"},
		Timestamp: time.Now().UTC(),
		Nonce:     "test-nonce-123",
	}

	secret := "test-secret"
	nc := &nonceCache{
		cache: make(map[string]time.Time),
		ttl:   10 * time.Minute,
	}

	// Sign the command
	if err := cmd.Sign(secret); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	// Tamper with the command
	cmd.Command = CommandStop

	// Verification should fail
	err := cmd.Verify(secret, 5*time.Minute, nc)
	if err == nil {
		t.Error("Verify succeeded after tampering (should fail)")
	}
}

func TestCommand_Verify_Timestamp(t *testing.T) {
	secret := "test-secret"
	nc := &nonceCache{
		cache: make(map[string]time.Time),
		ttl:   10 * time.Minute,
	}

	tests := []struct {
		name         string
		timestamp    time.Time
		maxClockSkew time.Duration
		shouldPass   bool
	}{
		{
			name:         "current timestamp",
			timestamp:    time.Now().UTC(),
			maxClockSkew: 5 * time.Minute,
			shouldPass:   true,
		},
		{
			name:         "timestamp 1 minute old",
			timestamp:    time.Now().UTC().Add(-1 * time.Minute),
			maxClockSkew: 5 * time.Minute,
			shouldPass:   true,
		},
		{
			name:         "timestamp 10 minutes old (expired)",
			timestamp:    time.Now().UTC().Add(-10 * time.Minute),
			maxClockSkew: 5 * time.Minute,
			shouldPass:   false,
		},
		{
			name:         "timestamp in future (clock skew)",
			timestamp:    time.Now().UTC().Add(2 * time.Minute),
			maxClockSkew: 5 * time.Minute,
			shouldPass:   true,
		},
		{
			name:         "timestamp too far in future",
			timestamp:    time.Now().UTC().Add(10 * time.Minute),
			maxClockSkew: 5 * time.Minute,
			shouldPass:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &Command{
				Command:   CommandRestart,
				Timestamp: tt.timestamp,
				Nonce:     "test-nonce-" + tt.name, // Unique nonce for each test
			}

			if err := cmd.Sign(secret); err != nil {
				t.Fatalf("Sign failed: %v", err)
			}

			err := cmd.Verify(secret, tt.maxClockSkew, nc)
			if tt.shouldPass && err != nil {
				t.Errorf("Verify failed but should pass: %v", err)
			}
			if !tt.shouldPass && err == nil {
				t.Error("Verify passed but should fail")
			}
		})
	}
}

func TestCommand_Verify_ReplayPrevention(t *testing.T) {
	cmd := &Command{
		Command:   CommandRestart,
		Timestamp: time.Now().UTC(),
		Nonce:     "test-nonce-replay",
	}

	secret := "test-secret"
	nc := &nonceCache{
		cache: make(map[string]time.Time),
		ttl:   10 * time.Minute,
	}

	// Sign the command
	if err := cmd.Sign(secret); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	// First verification should succeed
	if err := cmd.Verify(secret, 5*time.Minute, nc); err != nil {
		t.Errorf("First Verify failed: %v", err)
	}

	// Second verification with same nonce should fail (replay)
	err := cmd.Verify(secret, 5*time.Minute, nc)
	if err == nil {
		t.Error("Second Verify succeeded (replay attack should be detected)")
	}
}

func TestHandler_RegisterAndHandle(t *testing.T) {
	secret := "test-secret"
	handler := NewHandler(secret, 5*time.Minute, 10*time.Minute)

	// Track if command was executed
	executed := false
	handler.RegisterCommand(CommandRestart, func(params map[string]interface{}) error {
		executed = true
		return nil
	})

	// Create and sign command
	cmd := &Command{
		Command:   CommandRestart,
		Timestamp: time.Now().UTC(),
		Nonce:     "test-nonce-handler",
	}

	if err := cmd.Sign(secret); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	// Handle command
	result, err := handler.Handle(cmd)
	if err != nil {
		t.Errorf("Handle failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Command execution failed: %s", result.Message)
	}

	if !executed {
		t.Error("Command handler was not executed")
	}
}

func TestHandler_UnknownCommand(t *testing.T) {
	secret := "test-secret"
	handler := NewHandler(secret, 5*time.Minute, 10*time.Minute)

	// Create command for unregistered command type
	cmd := &Command{
		Command:   CommandRestart,
		Timestamp: time.Now().UTC(),
		Nonce:     "test-nonce-unknown",
	}

	if err := cmd.Sign(secret); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	// Handle should fail for unknown command
	result, err := handler.Handle(cmd)
	if err == nil {
		t.Error("Handle succeeded for unknown command (should fail)")
	}

	if result.Success {
		t.Error("Result shows success for unknown command")
	}
}

func TestNonceCache_Cleanup(t *testing.T) {
	nc := &nonceCache{
		cache: make(map[string]time.Time),
		ttl:   1 * time.Second,
	}

	// Add some nonces
	now := time.Now().UTC()
	nc.cache["old-nonce"] = now.Add(-2 * time.Second) // Expired
	nc.cache["new-nonce"] = now                       // Valid

	// Cleanup should remove expired nonces
	nc.cleanup()

	if _, exists := nc.cache["old-nonce"]; exists {
		t.Error("Expired nonce was not cleaned up")
	}

	if _, exists := nc.cache["new-nonce"]; !exists {
		t.Error("Valid nonce was incorrectly cleaned up")
	}
}

func TestNonceCache_CheckAndStore(t *testing.T) {
	nc := &nonceCache{
		cache: make(map[string]time.Time),
		ttl:   10 * time.Minute,
	}

	timestamp := time.Now().UTC()

	// First check should succeed and store
	if !nc.checkAndStore("nonce-1", timestamp) {
		t.Error("First checkAndStore failed (should succeed)")
	}

	// Second check with same nonce should fail
	if nc.checkAndStore("nonce-1", timestamp) {
		t.Error("Second checkAndStore succeeded (should fail for duplicate)")
	}

	// Different nonce should succeed
	if !nc.checkAndStore("nonce-2", timestamp) {
		t.Error("checkAndStore failed for different nonce")
	}
}
