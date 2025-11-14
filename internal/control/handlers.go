package control

import (
	"context"
	"fmt"
	"sync/atomic"

	"go.uber.org/zap"
)

// AgentController defines the interface for controlling agent lifecycle
type AgentController interface {
	// Stop gracefully shuts down the agent
	Stop(ctx context.Context) error
	// Restart performs a graceful restart
	Restart(ctx context.Context) error
	// SwitchMode switches between strict and passthrough privacy modes
	SwitchMode(mode string) error
	// GetMode returns the current privacy mode
	GetMode() string
}

// Handlers provides concrete implementations of control command handlers
type Handlers struct {
	controller AgentController
	logger     *zap.Logger

	// Track if shutdown is in progress
	shutdownInProgress atomic.Bool
}

// NewHandlers creates a new Handlers instance
func NewHandlers(controller AgentController, logger *zap.Logger) *Handlers {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Handlers{
		controller: controller,
		logger:     logger,
	}
}

// HandleStop handles the stop command
//
// Params: none
// Effect: Gracefully shuts down the agent
func (h *Handlers) HandleStop(params map[string]interface{}) error {
	if h.shutdownInProgress.Load() {
		return fmt.Errorf("shutdown already in progress")
	}

	h.shutdownInProgress.Store(true)
	defer h.shutdownInProgress.Store(false)

	h.logger.Info("executing stop command")

	ctx := context.Background()
	if err := h.controller.Stop(ctx); err != nil {
		h.logger.Error("stop command failed", zap.Error(err))
		return fmt.Errorf("stop failed: %w", err)
	}

	h.logger.Info("agent stopped successfully")
	return nil
}

// HandleStart handles the start command
//
// Params: none
// Effect: Attempts to start the agent (mainly used when agent is stopped)
//
// Note: In practice, this is less useful since a stopped agent cannot receive
// commands. This exists primarily for completeness and future webhook support.
func (h *Handlers) HandleStart(params map[string]interface{}) error {
	h.logger.Info("executing start command")
	// Starting is typically handled by the agent's main loop
	// This is a no-op for now - agent should already be running to receive this
	h.logger.Warn("start command received but agent is already running")
	return nil
}

// HandleRestart handles the restart command
//
// Params:
//   - mode (optional): "graceful" (default) or "immediate"
//
// Effect: Restarts the agent, optionally draining queues first
func (h *Handlers) HandleRestart(params map[string]interface{}) error {
	if h.shutdownInProgress.Load() {
		return fmt.Errorf("shutdown already in progress")
	}

	h.shutdownInProgress.Store(true)
	defer h.shutdownInProgress.Store(false)

	mode := "graceful"
	if modeParam, ok := params["mode"].(string); ok {
		mode = modeParam
	}

	h.logger.Info("executing restart command", zap.String("mode", mode))

	ctx := context.Background()
	if err := h.controller.Restart(ctx); err != nil {
		h.logger.Error("restart command failed", zap.Error(err))
		return fmt.Errorf("restart failed: %w", err)
	}

	h.logger.Info("agent restarted successfully")
	return nil
}

// HandleSwitchMode handles the switch_mode command
//
// Params:
//   - mode (required): "strict" or "passthrough"
//
// Effect: Switches the privacy mode without restarting
func (h *Handlers) HandleSwitchMode(params map[string]interface{}) error {
	mode, ok := params["mode"].(string)
	if !ok || mode == "" {
		return fmt.Errorf("mode parameter is required")
	}

	if mode != "strict" && mode != "passthrough" {
		return fmt.Errorf("invalid mode: %s (must be 'strict' or 'passthrough')", mode)
	}

	currentMode := h.controller.GetMode()
	if currentMode == mode {
		h.logger.Info("mode already set", zap.String("mode", mode))
		return nil
	}

	h.logger.Info("switching privacy mode",
		zap.String("from", currentMode),
		zap.String("to", mode),
	)

	if err := h.controller.SwitchMode(mode); err != nil {
		h.logger.Error("switch mode failed", zap.Error(err))
		return fmt.Errorf("switch mode failed: %w", err)
	}

	h.logger.Info("privacy mode switched successfully", zap.String("mode", mode))
	return nil
}

// HandleUpgrade handles the upgrade command
//
// Params:
//   - version (required): target version to upgrade to
//   - url (optional): custom download URL for the upgrade binary
//
// Effect: Downloads and applies agent upgrade
//
// Note: This is a placeholder for future implementation. Actual upgrade
// mechanism will depend on deployment method (binary, Docker, Kubernetes).
func (h *Handlers) HandleUpgrade(params map[string]interface{}) error {
	version, ok := params["version"].(string)
	if !ok || version == "" {
		return fmt.Errorf("version parameter is required")
	}

	h.logger.Info("upgrade command received",
		zap.String("target_version", version),
	)

	// TODO: Implement actual upgrade logic
	// This will need to:
	// 1. Validate version format
	// 2. Download new binary/image (with signature verification)
	// 3. Gracefully stop current agent
	// 4. Replace binary/trigger container update
	// 5. Start new version

	return fmt.Errorf("upgrade not yet implemented (target version: %s)", version)
}

// RegisterAll registers all command handlers with the given Handler
func (h *Handlers) RegisterAll(handler *Handler) {
	handler.RegisterCommand(CommandStop, h.HandleStop)
	handler.RegisterCommand(CommandStart, h.HandleStart)
	handler.RegisterCommand(CommandRestart, h.HandleRestart)
	handler.RegisterCommand(CommandSwitchMode, h.HandleSwitchMode)
	handler.RegisterCommand(CommandUpgrade, h.HandleUpgrade)
}
