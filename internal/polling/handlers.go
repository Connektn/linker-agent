package polling

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// AgentController defines the interface for controlling the agent
type AgentController interface {
	SwitchMode(mode string) error
	GetMode() string
	Restart(ctx context.Context) error
}

// Handlers provides command handler implementations
type Handlers struct {
	agent  AgentController
	logger *zap.Logger
}

// NewHandlers creates a new Handlers instance
func NewHandlers(agent AgentController, logger *zap.Logger) *Handlers {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Handlers{
		agent:  agent,
		logger: logger,
	}
}

// HandleSwitchMode handles the switch_mode command
func (h *Handlers) HandleSwitchMode(ctx context.Context, params map[string]interface{}) error {
	mode, ok := params["mode"].(string)
	if !ok || mode == "" {
		return fmt.Errorf("mode parameter is required")
	}

	if mode != "strict" && mode != "passthrough" {
		return fmt.Errorf("invalid mode: %s (must be 'strict' or 'passthrough')", mode)
	}

	currentMode := h.agent.GetMode()
	if currentMode == mode {
		h.logger.Info("mode already set", zap.String("mode", mode))
		return nil
	}

	h.logger.Info("switching privacy mode",
		zap.String("from", currentMode),
		zap.String("to", mode),
	)

	if err := h.agent.SwitchMode(mode); err != nil {
		h.logger.Error("switch mode failed", zap.Error(err))
		return fmt.Errorf("switch mode failed: %w", err)
	}

	h.logger.Info("privacy mode switched successfully", zap.String("mode", mode))
	return nil
}

// HandleRestart handles the restart command
func (h *Handlers) HandleRestart(ctx context.Context, params map[string]interface{}) error {
	h.logger.Info("executing restart command")

	if err := h.agent.Restart(ctx); err != nil {
		h.logger.Error("restart command failed", zap.Error(err))
		return fmt.Errorf("restart failed: %w", err)
	}

	h.logger.Info("restart command executed successfully")
	return nil
}

// HandleUpgrade handles the upgrade command (not implemented in v1)
func (h *Handlers) HandleUpgrade(ctx context.Context, params map[string]interface{}) error {
	version, ok := params["version"].(string)
	if !ok || version == "" {
		return fmt.Errorf("version parameter is required")
	}

	h.logger.Info("upgrade command received",
		zap.String("target_version", version),
	)

	// TODO: Implement upgrade logic in future version
	// This will need to:
	// 1. Download new binary from downloadUrl
	// 2. Verify checksum/signature
	// 3. Replace current binary
	// 4. Restart with new version

	return fmt.Errorf("upgrade not yet implemented (target version: %s)", version)
}
