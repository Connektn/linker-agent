package heartbeat

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
		LastFlushTS:    time.Now().UTC().Unix(),
		Timestamp:      time.Now().UTC().Unix(),
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
		LastFlushTS:    time.Now().UTC().Unix(),
		Timestamp:      time.Now().UTC().Unix(),
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
		LastFlushTS:    time.Now().UTC().Unix(),
		Timestamp:      time.Now().UTC().Unix(),
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
		Timestamp: time.Now().UTC().Unix(),
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
		Timestamp: time.Now().UTC().Unix(),
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
		Timestamp: time.Now().UTC().Unix(),
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

	if payload.Timestamp == 0 {
		t.Error("Timestamp is zero")
	}
}

func TestPayload_SignatureConsistency(t *testing.T) {
	// Same payload should produce same signature
	ts := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC).Unix()

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
		LastFlushTS:    ts,
		Timestamp:      ts,
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
		LastFlushTS:    ts,
		Timestamp:      ts,
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

func TestSignRequest(t *testing.T) {
	// Secret must be base64-encoded (this is "test-secret-key" as raw bytes, then base64-encoded)
	secret := base64.StdEncoding.EncodeToString([]byte("test-secret-key"))
	timestamp := int64(1234567890)
	nonce := "dGVzdC1ub25jZQ==" // "test-nonce" in base64
	body := []byte(`{"agentId":"test123","timestamp":1234567890}`)

	signature, err := SignRequest(secret, timestamp, nonce, body)
	if err != nil {
		t.Fatalf("SignRequest failed: %v", err)
	}

	if signature == "" {
		t.Error("Signature is empty")
	}

	// Verify signature is base64-encoded
	_, err = base64.StdEncoding.DecodeString(signature)
	if err != nil {
		t.Errorf("Signature is not valid base64: %v", err)
	}
}

func TestSignRequest_EmptySecret(t *testing.T) {
	timestamp := int64(1234567890)
	nonce := "test-nonce"
	body := []byte(`{"test":"data"}`)

	_, err := SignRequest("", timestamp, nonce, body)
	if err == nil {
		t.Error("Expected error for empty secret, got nil")
	}
}

func TestSignRequest_FormatMatch(t *testing.T) {
	// Test that SignRequest produces the expected format: timestamp|nonce|body
	secretRaw := []byte("test-secret-key")
	secret := base64.StdEncoding.EncodeToString(secretRaw)
	timestamp := int64(1234567890)
	nonce := "abc123"
	body := []byte(`{"test":"data"}`)

	signature, err := SignRequest(secret, timestamp, nonce, body)
	if err != nil {
		t.Fatalf("SignRequest failed: %v", err)
	}

	// Manually compute expected signature (matching server's algorithm)
	message := fmt.Sprintf("%d|%s|%s", timestamp, nonce, string(body))
	mac := hmac.New(sha256.New, secretRaw) // Use raw bytes, not base64
	mac.Write([]byte(message))
	expectedSig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if signature != expectedSig {
		t.Errorf("Signature mismatch:\ngot:  %s\nwant: %s", signature, expectedSig)
	}
}

func TestSignRequest_Consistency(t *testing.T) {
	// Same inputs should produce same signature
	secret := base64.StdEncoding.EncodeToString([]byte("test-secret"))
	timestamp := int64(9876543210)
	nonce := "nonce123"
	body := []byte(`{"agent":"test"}`)

	sig1, err := SignRequest(secret, timestamp, nonce, body)
	if err != nil {
		t.Fatalf("First SignRequest failed: %v", err)
	}

	sig2, err := SignRequest(secret, timestamp, nonce, body)
	if err != nil {
		t.Fatalf("Second SignRequest failed: %v", err)
	}

	if sig1 != sig2 {
		t.Errorf("Signatures differ for identical inputs:\n%s\n%s", sig1, sig2)
	}
}

func TestSignRequest_DifferentInputs(t *testing.T) {
	// Different inputs should produce different signatures
	secret := base64.StdEncoding.EncodeToString([]byte("test-secret"))
	timestamp := int64(1234567890)
	nonce := "nonce"
	body1 := []byte(`{"agent":"test1"}`)
	body2 := []byte(`{"agent":"test2"}`)

	sig1, _ := SignRequest(secret, timestamp, nonce, body1)
	sig2, _ := SignRequest(secret, timestamp, nonce, body2)

	if sig1 == sig2 {
		t.Error("Signatures are identical for different bodies (should differ)")
	}

	// Different timestamp
	sig3, _ := SignRequest(secret, timestamp+1, nonce, body1)
	if sig1 == sig3 {
		t.Error("Signatures are identical for different timestamps (should differ)")
	}

	// Different nonce
	sig4, _ := SignRequest(secret, timestamp, "different-nonce", body1)
	if sig1 == sig4 {
		t.Error("Signatures are identical for different nonces (should differ)")
	}
}

func TestSignRequest_ServerCompatibility(t *testing.T) {
	// Test that the signature format matches what the server expects
	// Server decodes secret from base64 before use
	secretRaw := []byte("shared-secret-bytes-for-hmac")
	secret := base64.StdEncoding.EncodeToString(secretRaw)
	timestamp := int64(1735998660)
	nonce := "YWJjZGVmZ2hpamtsbW5vcA==" // base64 nonce

	payload := Payload{
		AgentID:        "agent_123",
		OrganizationID: "org_456",
		Version:        "1.0.0",
		Uptime:         3600,
		Mode:           "strict",
		QueueDepth:     5,
		DLQSize:        0,
		DroppedCount:   0,
		RetryCount:     0,
		LastFlushTS:    1735998000,
		Timestamp:      timestamp,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	signature, err := SignRequest(secret, timestamp, nonce, body)
	if err != nil {
		t.Fatalf("SignRequest failed: %v", err)
	}

	// The server should be able to verify this signature by computing:
	// 1. Decode secret from base64
	// 2. HMAC-SHA256(decodedSecret, "timestamp|nonce|body")
	expectedMessage := fmt.Sprintf("%d|%s|%s", timestamp, nonce, string(body))
	mac := hmac.New(sha256.New, secretRaw) // Use raw bytes like server does
	mac.Write([]byte(expectedMessage))
	serverSig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if signature != serverSig {
		t.Errorf("Server-side verification would fail:\nclient: %s\nserver: %s", signature, serverSig)
	}
}

func TestSignRequest_InvalidBase64Secret(t *testing.T) {
	// Test that invalid base64 secret returns error
	invalidSecret := "not-valid-base64!!!"
	timestamp := int64(1234567890)
	nonce := "test-nonce"
	body := []byte(`{"test":"data"}`)

	_, err := SignRequest(invalidSecret, timestamp, nonce, body)
	if err == nil {
		t.Error("Expected error for invalid base64 secret, got nil")
	}
}
