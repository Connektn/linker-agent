package heartbeat

import (
	"testing"
	"time"
)

func TestPayload_Sign(t *testing.T) {
	payload := &Payload{
		AgentID:        "agent_test123",
		OrganizationID: "org_456",
		Version:        "1.0.0",
		Uptime:         3600,
		Mode:           "strict",
		QueueDepth:     10,
		DLQSize:        2,
		DroppedCount:   5,
		RetryCount:     3,
		LastFlushTS:    time.Now().UTC(),
		Timestamp:      time.Now().UTC(),
	}

	secret := "test-secret-key"

	// Sign the payload
	err := payload.Sign(secret)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	if payload.Signature == "" {
		t.Error("Signature is empty after signing")
	}
}

func TestPayload_Verify(t *testing.T) {
	payload := &Payload{
		AgentID:        "agent_test123",
		OrganizationID: "org_456",
		Version:        "1.0.0",
		Uptime:         3600,
		Mode:           "strict",
		QueueDepth:     10,
		DLQSize:        2,
		DroppedCount:   5,
		RetryCount:     3,
		LastFlushTS:    time.Now().UTC(),
		Timestamp:      time.Now().UTC(),
	}

	secret := "test-secret-key"

	// Sign the payload
	if err := payload.Sign(secret); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	// Verify with correct secret
	if !payload.Verify(secret) {
		t.Error("Verify failed with correct secret")
	}

	// Verify with wrong secret
	if payload.Verify("wrong-secret") {
		t.Error("Verify succeeded with wrong secret (should fail)")
	}
}

func TestPayload_Verify_Tampering(t *testing.T) {
	payload := &Payload{
		AgentID:        "agent_test123",
		OrganizationID: "org_456",
		Version:        "1.0.0",
		Uptime:         3600,
		Mode:           "strict",
		QueueDepth:     10,
		DLQSize:        2,
		DroppedCount:   5,
		RetryCount:     3,
		LastFlushTS:    time.Now().UTC(),
		Timestamp:      time.Now().UTC(),
	}

	secret := "test-secret-key"

	// Sign the payload
	if err := payload.Sign(secret); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	// Tamper with the payload
	payload.QueueDepth = 999

	// Verification should fail
	if payload.Verify(secret) {
		t.Error("Verify succeeded after tampering (should fail)")
	}
}

func TestPayload_Sign_EmptySecret(t *testing.T) {
	payload := &Payload{
		AgentID:   "agent_test123",
		Timestamp: time.Now().UTC(),
	}

	// Sign with empty secret should fail
	err := payload.Sign("")
	if err == nil {
		t.Error("Expected error for empty secret, got nil")
	}
}

func TestPayload_Verify_EmptySecret(t *testing.T) {
	payload := &Payload{
		AgentID:   "agent_test123",
		Signature: "fake-signature",
		Timestamp: time.Now().UTC(),
	}

	// Verify with empty secret should return false
	if payload.Verify("") {
		t.Error("Verify succeeded with empty secret (should fail)")
	}
}

func TestPayload_Verify_EmptySignature(t *testing.T) {
	payload := &Payload{
		AgentID:   "agent_test123",
		Signature: "",
		Timestamp: time.Now().UTC(),
	}

	// Verify with empty signature should return false
	if payload.Verify("some-secret") {
		t.Error("Verify succeeded with empty signature (should fail)")
	}
}

func TestNewPayload(t *testing.T) {
	startTime := time.Now().Add(-1 * time.Hour)
	metrics := QueueMetrics{
		Depth:        15,
		DLQSize:      3,
		DroppedCount: 10,
		RetryCount:   5,
	}
	lastFlush := time.Now().Add(-30 * time.Second)

	payload := NewPayload(
		"agent_abc",
		"org_xyz",
		"2.0.0",
		startTime,
		"passthrough",
		metrics,
		lastFlush,
	)

	if payload.AgentID != "agent_abc" {
		t.Errorf("AgentID = %s, want agent_abc", payload.AgentID)
	}

	if payload.OrganizationID != "org_xyz" {
		t.Errorf("OrganizationID = %s, want org_xyz", payload.OrganizationID)
	}

	if payload.Version != "2.0.0" {
		t.Errorf("Version = %s, want 2.0.0", payload.Version)
	}

	if payload.Mode != "passthrough" {
		t.Errorf("Mode = %s, want passthrough", payload.Mode)
	}

	if payload.QueueDepth != 15 {
		t.Errorf("QueueDepth = %d, want 15", payload.QueueDepth)
	}

	if payload.DLQSize != 3 {
		t.Errorf("DLQSize = %d, want 3", payload.DLQSize)
	}

	if payload.DroppedCount != 10 {
		t.Errorf("DroppedCount = %d, want 10", payload.DroppedCount)
	}

	if payload.RetryCount != 5 {
		t.Errorf("RetryCount = %d, want 5", payload.RetryCount)
	}

	// Uptime should be approximately 3600 seconds (1 hour)
	if payload.Uptime < 3595 || payload.Uptime > 3605 {
		t.Errorf("Uptime = %d, want ~3600", payload.Uptime)
	}

	if payload.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
}

func TestPayload_SignatureConsistency(t *testing.T) {
	// Same payload should produce same signature
	payload1 := &Payload{
		AgentID:        "agent_test",
		OrganizationID: "org_test",
		Version:        "1.0.0",
		Uptime:         100,
		Mode:           "strict",
		QueueDepth:     5,
		DLQSize:        1,
		DroppedCount:   0,
		RetryCount:     0,
		LastFlushTS:    time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Timestamp:      time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
	}

	payload2 := &Payload{
		AgentID:        "agent_test",
		OrganizationID: "org_test",
		Version:        "1.0.0",
		Uptime:         100,
		Mode:           "strict",
		QueueDepth:     5,
		DLQSize:        1,
		DroppedCount:   0,
		RetryCount:     0,
		LastFlushTS:    time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Timestamp:      time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
	}

	secret := "test-secret"

	if err := payload1.Sign(secret); err != nil {
		t.Fatalf("Sign payload1 failed: %v", err)
	}

	if err := payload2.Sign(secret); err != nil {
		t.Fatalf("Sign payload2 failed: %v", err)
	}

	if payload1.Signature != payload2.Signature {
		t.Errorf("Signatures differ for identical payloads:\n%s\n%s", payload1.Signature, payload2.Signature)
	}
}
