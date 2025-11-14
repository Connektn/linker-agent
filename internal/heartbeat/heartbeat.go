package heartbeat

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// Payload represents the heartbeat data sent to Connektn Cloud.
// Contains agent health, status, and operational metrics.
type Payload struct {
	AgentID        string    `json:"agentId"`
	OrganizationID string    `json:"organizationId"`
	Version        string    `json:"version"`
	Uptime         int64     `json:"uptimeSeconds"`
	Mode           string    `json:"mode"` // "strict" or "passthrough"
	QueueDepth     int       `json:"queueDepth"`
	DLQSize        int       `json:"dlqSize"`
	DroppedCount   uint64    `json:"droppedCount"`
	RetryCount     int       `json:"retryCount"`
	LastFlushTS    int64     `json:"lastFlushTs"` // Unix timestamp (seconds since epoch)
	Timestamp      int64     `json:"timestamp"`   // Unix timestamp (seconds since epoch)
	Signature      string    `json:"signature,omitempty"` // HMAC-SHA256 signature
}

// QueueMetrics holds queue-related metrics for heartbeat
type QueueMetrics struct {
	Depth        int    // Current queue depth
	DLQSize      int    // Dead letter queue size
	DroppedCount uint64 // Total dropped items
	RetryCount   int    // Total retry attempts
}

// SignRequest generates an HMAC-SHA256 signature for the heartbeat request.
//
// Threat model:
// - Prevents payload tampering in transit
// - Prevents replay attacks via nonce and timestamp
// - Allows cloud to verify authenticity of heartbeat
// - Secret must be securely shared during agent onboarding
//
// The signature is computed over "timestamp|nonce|body" using HMAC-SHA256,
// matching the server's expected format for verification.
//
// IMPORTANT: The secret parameter must be a base64-encoded string. It will be
// decoded before use as the HMAC key, matching the server's implementation.
//
// Parameters:
//   - secret: the shared secret key for HMAC (base64-encoded)
//   - timestamp: Unix timestamp (seconds since epoch)
//   - nonce: base64-encoded random nonce for replay protection
//   - body: JSON-marshaled payload bytes
//
// Returns:
//   - string: base64-encoded HMAC-SHA256 signature
//   - error: if secret is empty or invalid base64
func SignRequest(secret string, timestamp int64, nonce string, body []byte) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("heartbeat: signature secret is empty")
	}

	// Decode the base64-encoded secret to get the raw key bytes
	// This matches the server's implementation which decodes the secret before use
	secretBytes, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return "", fmt.Errorf("heartbeat: failed to decode secret from base64: %w", err)
	}

	// Construct message: timestamp|nonce|body
	message := fmt.Sprintf("%d|%s|%s", timestamp, nonce, string(body))

	// Compute HMAC-SHA256 using the decoded secret bytes
	mac := hmac.New(sha256.New, secretBytes)
	mac.Write([]byte(message))
	signature := mac.Sum(nil)

	// Encode as base64
	return base64.StdEncoding.EncodeToString(signature), nil
}

// Sign generates an HMAC-SHA256 signature for the heartbeat payload.
//
// DEPRECATED: Use SignRequest instead for proper timestamp|nonce|body format.
//
// Threat model:
// - Prevents payload tampering in transit
// - Allows cloud to verify authenticity of heartbeat
// - Secret must be securely shared during agent onboarding
//
// The signature is computed over the JSON representation of the payload
// (excluding the signature field itself) using HMAC-SHA256.
func (p *Payload) Sign(secret string) error {
	if secret == "" {
		return fmt.Errorf("heartbeat: signature secret is empty")
	}

	// Create a copy without signature for signing
	unsigned := *p
	unsigned.Signature = ""

	// Marshal to JSON
	data, err := json.Marshal(unsigned)
	if err != nil {
		return fmt.Errorf("heartbeat: failed to marshal payload: %w", err)
	}

	// Compute HMAC
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(data)
	signature := mac.Sum(nil)

	// Encode as base64
	p.Signature = base64.StdEncoding.EncodeToString(signature)

	return nil
}

// Verify checks if the payload signature is valid.
//
// Returns true if the signature is valid, false otherwise.
// Used by cloud-side to verify heartbeat authenticity.
func (p *Payload) Verify(secret string) bool {
	if secret == "" || p.Signature == "" {
		return false
	}

	// Decode the signature
	receivedSig, err := base64.StdEncoding.DecodeString(p.Signature)
	if err != nil {
		return false
	}

	// Create a copy without signature for verification
	unsigned := *p
	unsigned.Signature = ""

	// Marshal to JSON
	data, err := json.Marshal(unsigned)
	if err != nil {
		return false
	}

	// Compute expected HMAC
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(data)
	expectedSig := mac.Sum(nil)

	// Compare using constant-time comparison to prevent timing attacks
	return hmac.Equal(receivedSig, expectedSig)
}

// NewPayload creates a new heartbeat payload with the given parameters
func NewPayload(
	agentID string,
	orgID string,
	version string,
	startTime time.Time,
	mode string,
	metrics QueueMetrics,
	lastFlushTS time.Time,
) *Payload {
	return &Payload{
		AgentID:        agentID,
		OrganizationID: orgID,
		Version:        version,
		Uptime:         int64(time.Since(startTime).Seconds()),
		Mode:           mode,
		QueueDepth:     metrics.Depth,
		DLQSize:        metrics.DLQSize,
		DroppedCount:   metrics.DroppedCount,
		RetryCount:     metrics.RetryCount,
		LastFlushTS:    lastFlushTS.Unix(),
		Timestamp:      time.Now().UTC().Unix(),
	}
}
