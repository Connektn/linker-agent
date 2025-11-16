// Package polling implements command polling for the Connektn Linker Agent.
// The agent actively polls the CDP backend for control commands at regular intervals.
//
// Security: Uses HMAC-SHA256 authentication with control secret (separate from heartbeat secret).
// All HTTP requests include timestamp, nonce, and signature headers for replay protection.
package polling

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"linker-agent/internal/heartbeat"

	"go.uber.org/zap"
)

// CommandType represents the type of control command
type CommandType string

const (
	// CommandSwitchMode switches privacy mode
	CommandSwitchMode CommandType = "switch_mode"
	// CommandRestart performs graceful restart
	CommandRestart CommandType = "restart"
	// CommandUpgrade upgrades agent version (optional, not implemented in v1)
	CommandUpgrade CommandType = "upgrade"
)

// Command represents a command from the CDP backend
type Command struct {
	CommandID string                 `json:"commandId"`
	Command   CommandType            `json:"command"`
	Params    map[string]interface{} `json:"params,omitempty"`
}

// PollRequest is sent to the CDP when polling for commands
type PollRequest struct {
	AgentID        string `json:"agentId"`
	OrganizationID string `json:"organizationId"`
}

// AckRequest is sent to acknowledge command execution
type AckRequest struct {
	AgentID        string `json:"agentId"`
	OrganizationID string `json:"organizationId"`
	CommandID      string `json:"commandId"`
	Status         string `json:"status"` // "success" or "error"
	Message        string `json:"message,omitempty"`
}

// CommandHandler processes a specific command type
type CommandHandler func(ctx context.Context, params map[string]interface{}) error

// Poller polls the CDP backend for commands and executes them
type Poller struct {
	cdpBaseURL     string
	agentID        string
	organizationID string
	secret         string
	interval       time.Duration
	timeout        time.Duration
	httpClient     *http.Client
	logger         *zap.Logger
	handlers       map[CommandType]CommandHandler
}

// PollerConfig holds configuration for the command poller
type PollerConfig struct {
	CDPBaseURL     string
	AgentID        string
	OrganizationID string
	Secret         string
	Interval       time.Duration
	Timeout        time.Duration
	Logger         *zap.Logger
}

// NewPoller creates a new command poller
func NewPoller(cfg PollerConfig) (*Poller, error) {
	if cfg.CDPBaseURL == "" {
		return nil, fmt.Errorf("CDP base URL is required")
	}
	if cfg.AgentID == "" {
		return nil, fmt.Errorf("agent ID is required")
	}
	if cfg.OrganizationID == "" {
		return nil, fmt.Errorf("organization ID is required")
	}
	if cfg.Secret == "" {
		return nil, fmt.Errorf("control secret is required")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}

	return &Poller{
		cdpBaseURL:     cfg.CDPBaseURL,
		agentID:        cfg.AgentID,
		organizationID: cfg.OrganizationID,
		secret:         cfg.Secret,
		interval:       cfg.Interval,
		timeout:        cfg.Timeout,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		logger:   cfg.Logger,
		handlers: make(map[CommandType]CommandHandler),
	}, nil
}

// RegisterHandler registers a handler for a specific command type
func (p *Poller) RegisterHandler(cmdType CommandType, handler CommandHandler) {
	p.handlers[cmdType] = handler
}

// Start begins the polling loop
func (p *Poller) Start(ctx context.Context) {
	p.logger.Info("starting command poller",
		zap.String("interval", p.interval.String()),
		zap.String("cdp_base_url", p.cdpBaseURL),
	)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	// Poll immediately on start, then at intervals
	p.pollOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("command poller stopped")
			return
		case <-ticker.C:
			p.pollOnce(ctx)
		}
	}
}

// pollOnce executes a single poll cycle
func (p *Poller) pollOnce(ctx context.Context) {
	cmd, err := p.poll(ctx)
	if err != nil {
		p.logger.Warn("command poll failed", zap.Error(err))
		return
	}

	if cmd == nil {
		// No command available (204 No Content)
		return
	}

	p.logger.Info("received command",
		zap.String("command_id", cmd.CommandID),
		zap.String("command", string(cmd.Command)),
	)

	// Execute command
	if err := p.executeCommand(ctx, cmd); err != nil {
		p.logger.Error("command execution failed",
			zap.String("command_id", cmd.CommandID),
			zap.String("command", string(cmd.Command)),
			zap.Error(err),
		)

		// Acknowledge with error
		p.acknowledge(ctx, cmd.CommandID, "error", err.Error())
		return
	}

	p.logger.Info("command executed successfully",
		zap.String("command_id", cmd.CommandID),
		zap.String("command", string(cmd.Command)),
	)

	// Acknowledge with success
	p.acknowledge(ctx, cmd.CommandID, "success", "")

	// Poll immediately after command execution to check for next command
	p.pollOnce(ctx)
}

// poll sends a poll request to the CDP and returns a command if available
func (p *Poller) poll(ctx context.Context) (*Command, error) {
	pollURL := fmt.Sprintf("%s/api/agent/command/poll", p.cdpBaseURL)

	// Create request body
	reqBody := PollRequest{
		AgentID:        p.agentID,
		OrganizationID: p.organizationID,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal poll request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pollURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create poll request: %w", err)
	}

	// Add authentication headers
	timestamp := time.Now().Unix()
	nonce, err := generateNonce()
	if err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	signature, err := heartbeat.SignRequest(p.secret, timestamp, nonce, bodyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to sign request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", fmt.Sprintf("%d", timestamp))
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Signature", signature)

	// Send request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poll request failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle 204 No Content (no commands available)
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	// Handle errors
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("poll request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse command
	var cmd Command
	if err := json.NewDecoder(resp.Body).Decode(&cmd); err != nil {
		return nil, fmt.Errorf("failed to decode command: %w", err)
	}

	// Check if command is empty (server returned {} instead of 204)
	if cmd.CommandID == "" || cmd.Command == "" {
		return nil, nil
	}

	return &cmd, nil
}

// acknowledge sends an acknowledgment for a command
func (p *Poller) acknowledge(ctx context.Context, commandID, status, errorMessage string) {
	ackURL := fmt.Sprintf("%s/api/agent/command/result", p.cdpBaseURL)

	// Create request body
	reqBody := AckRequest{
		AgentID:        p.agentID,
		OrganizationID: p.organizationID,
		CommandID:      commandID,
		Status:         status,
		Message:        errorMessage,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		p.logger.Error("failed to marshal ack request", zap.Error(err))
		return
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ackURL, bytes.NewReader(bodyBytes))
	if err != nil {
		p.logger.Error("failed to create ack request", zap.Error(err))
		return
	}

	// Add authentication headers
	timestamp := time.Now().Unix()
	nonce, err := generateNonce()
	if err != nil {
		p.logger.Error("failed to generate nonce for ack", zap.Error(err))
		return
	}

	signature, err := heartbeat.SignRequest(p.secret, timestamp, nonce, bodyBytes)
	if err != nil {
		p.logger.Error("failed to sign ack request", zap.Error(err))
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", fmt.Sprintf("%d", timestamp))
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Signature", signature)

	// Send request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.logger.Error("ack request failed", zap.Error(err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		p.logger.Error("ack request failed",
			zap.Int("status_code", resp.StatusCode),
			zap.String("body", string(body)),
		)
		return
	}

	p.logger.Info("command acknowledged",
		zap.String("command_id", commandID),
		zap.String("status", status),
	)
}

// executeCommand executes a command by calling the registered handler
func (p *Poller) executeCommand(ctx context.Context, cmd *Command) error {
	handler, ok := p.handlers[cmd.Command]
	if !ok {
		return fmt.Errorf("unknown command type: %s", cmd.Command)
	}

	return handler(ctx, cmd.Params)
}

// generateNonce generates a cryptographically secure random nonce
func generateNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}
