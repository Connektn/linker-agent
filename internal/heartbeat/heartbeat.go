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
	AgentID        string    `json:"agent_id"`
	OrganizationID string    `json:"organization_id"`
	Version        string    `json:"version"`
	Uptime         int64     `json:"uptime_seconds"`
	Mode           string    `json:"mode"` // "strict" or "passthrough"
	QueueDepth     int       `json:"queue_depth"`
	DLQSize        int       `json:"dlq_size"`
	DroppedCount   uint64    `json:"dropped_count"`
	RetryCount     int       `json:"retry_count"`
	LastFlushTS    time.Time `json:"last_flush_ts"`
	Timestamp      time.Time `json:"timestamp"`
	Signature      string    `json:"signature,omitempty"` // HMAC-SHA256 signature
}

// QueueMetrics holds queue-related metrics for heartbeat
type QueueMetrics struct {
	Depth        int    // Current queue depth
	DLQSize      int    // Dead letter queue size
	DroppedCount uint64 // Total dropped items
	RetryCount   int    // Total retry attempts
}

// Sign generates an HMAC-SHA256 signature for the heartbeat payload.
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
		LastFlushTS:    lastFlushTS,
		Timestamp:      time.Now().UTC(),
	}
}
