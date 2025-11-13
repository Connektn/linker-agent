package control

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// CommandType represents the type of control command
type CommandType string

const (
	// CommandStop gracefully shuts down the agent
	CommandStop CommandType = "stop"
	// CommandStart attempts to start the agent (used when no heartbeats)
	CommandStart CommandType = "start"
	// CommandRestart performs stop + start as one operation
	CommandRestart CommandType = "restart"
	// CommandSwitchMode switches between strict and passthrough privacy modes
	CommandSwitchMode CommandType = "switch_mode"
	// CommandUpgrade upgrades the agent to a new version
	CommandUpgrade CommandType = "upgrade"
)

// Command represents a control command from Connektn Cloud
type Command struct {
	Command   CommandType            `json:"command"`
	Params    map[string]interface{} `json:"params,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Nonce     string                 `json:"nonce"`
	Signature string                 `json:"signature"`
}

// CommandResult represents the result of executing a control command
type CommandResult struct {
	Command   CommandType `json:"command"`
	Success   bool        `json:"success"`
	Message   string      `json:"message"`
	Timestamp time.Time   `json:"timestamp"`
}

// Handler processes control commands with signature verification and replay protection
type Handler struct {
	secret        string
	maxClockSkew  time.Duration
	nonceCache    *nonceCache
	commandRouter map[CommandType]CommandFunc
	mu            sync.RWMutex
}

// CommandFunc is a function that executes a control command
type CommandFunc func(params map[string]interface{}) error

// nonceCache stores recently seen nonces to prevent replay attacks
type nonceCache struct {
	cache map[string]time.Time
	ttl   time.Duration
	mu    sync.RWMutex
}

// NewHandler creates a new control command handler
//
// Threat model:
// - Signature verification prevents unauthorized commands
// - Nonce cache prevents replay attacks
// - Timestamp validation prevents delayed replay
// - Secret must be securely shared during agent onboarding
func NewHandler(secret string, maxClockSkew time.Duration, nonceCacheTTL time.Duration) *Handler {
	return &Handler{
		secret:       secret,
		maxClockSkew: maxClockSkew,
		nonceCache: &nonceCache{
			cache: make(map[string]time.Time),
			ttl:   nonceCacheTTL,
		},
		commandRouter: make(map[CommandType]CommandFunc),
	}
}

// RegisterCommand registers a handler function for a specific command type
func (h *Handler) RegisterCommand(cmdType CommandType, fn CommandFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.commandRouter[cmdType] = fn
}

// Verify checks if the command signature is valid and not replayed
//
// Returns an error if verification fails, nil if command is valid
func (c *Command) Verify(secret string, maxClockSkew time.Duration, nonceCache *nonceCache) error {
	if secret == "" {
		return fmt.Errorf("control: signature secret is empty")
	}

	if c.Signature == "" {
		return fmt.Errorf("control: signature is missing")
	}

	// Check timestamp (prevent delayed replay)
	now := time.Now().UTC()
	timeDiff := now.Sub(c.Timestamp)
	if timeDiff < 0 {
		timeDiff = -timeDiff
	}

	if timeDiff > maxClockSkew {
		return fmt.Errorf("control: command timestamp outside acceptable window (diff: %v, max: %v)", timeDiff, maxClockSkew)
	}

	// Check nonce (prevent immediate replay)
	if !nonceCache.checkAndStore(c.Nonce, c.Timestamp) {
		return fmt.Errorf("control: nonce already seen (replay attack)")
	}

	// Verify signature
	if !c.verifySignature(secret) {
		return fmt.Errorf("control: signature verification failed")
	}

	return nil
}

// verifySignature checks if the command signature is valid
func (c *Command) verifySignature(secret string) bool {
	// Decode the signature
	receivedSig, err := base64.StdEncoding.DecodeString(c.Signature)
	if err != nil {
		return false
	}

	// Create a copy without signature for verification
	unsigned := *c
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

// Sign generates an HMAC-SHA256 signature for the command
func (c *Command) Sign(secret string) error {
	if secret == "" {
		return fmt.Errorf("control: signature secret is empty")
	}

	// Create a copy without signature for signing
	unsigned := *c
	unsigned.Signature = ""

	// Marshal to JSON
	data, err := json.Marshal(unsigned)
	if err != nil {
		return fmt.Errorf("control: failed to marshal command: %w", err)
	}

	// Compute HMAC
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(data)
	signature := mac.Sum(nil)

	// Encode as base64
	c.Signature = base64.StdEncoding.EncodeToString(signature)

	return nil
}

// Handle processes a control command
func (h *Handler) Handle(cmd *Command) (*CommandResult, error) {
	// Verify command signature and replay protection
	if err := cmd.Verify(h.secret, h.maxClockSkew, h.nonceCache); err != nil {
		return &CommandResult{
			Command:   cmd.Command,
			Success:   false,
			Message:   fmt.Sprintf("Verification failed: %v", err),
			Timestamp: time.Now().UTC(),
		}, err
	}

	// Get command handler
	h.mu.RLock()
	handler, exists := h.commandRouter[cmd.Command]
	h.mu.RUnlock()

	if !exists {
		err := fmt.Errorf("unknown command: %s", cmd.Command)
		return &CommandResult{
			Command:   cmd.Command,
			Success:   false,
			Message:   err.Error(),
			Timestamp: time.Now().UTC(),
		}, err
	}

	// Execute command
	if err := handler(cmd.Params); err != nil {
		return &CommandResult{
			Command:   cmd.Command,
			Success:   false,
			Message:   fmt.Sprintf("Execution failed: %v", err),
			Timestamp: time.Now().UTC(),
		}, err
	}

	return &CommandResult{
		Command:   cmd.Command,
		Success:   true,
		Message:   "Command executed successfully",
		Timestamp: time.Now().UTC(),
	}, nil
}

// checkAndStore checks if a nonce has been seen and stores it if not
func (nc *nonceCache) checkAndStore(nonce string, timestamp time.Time) bool {
	nc.mu.Lock()
	defer nc.mu.Unlock()

	// Clean up expired nonces
	nc.cleanup()

	// Check if nonce exists
	if _, exists := nc.cache[nonce]; exists {
		return false // Nonce already seen (replay)
	}

	// Store nonce with timestamp
	nc.cache[nonce] = timestamp
	return true
}

// cleanup removes expired nonces from the cache
func (nc *nonceCache) cleanup() {
	now := time.Now().UTC()
	for nonce, timestamp := range nc.cache {
		if now.Sub(timestamp) > nc.ttl {
			delete(nc.cache, nonce)
		}
	}
}
