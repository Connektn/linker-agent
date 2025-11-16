package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"linker-agent/internal/state"

	"go.uber.org/zap"
)

// Agent represents the main agent with lifecycle control and heartbeat management
type Agent struct {
	// Identity
	id             string
	organizationID string
	version        string

	// State
	startTime    time.Time
	mode         atomic.Value // string: "strict" or "passthrough"
	stateManager *state.Manager
	mu           sync.RWMutex

	// Components
	logger *zap.Logger

	// Shutdown coordination
	shutdownFn context.CancelFunc
	shutdownMu sync.Mutex
}

// Config holds agent configuration
type Config struct {
	AgentID        string
	OrganizationID string
	Version        string
	InitialMode    string // "strict" or "passthrough"
	StateManager   *state.Manager
	Logger         *zap.Logger
}

// New creates a new Agent instance
func New(cfg Config) (*Agent, error) {
	if cfg.AgentID == "" {
		return nil, fmt.Errorf("agent ID is required")
	}
	if cfg.OrganizationID == "" {
		return nil, fmt.Errorf("organization ID is required")
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}

	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}

	// Determine initial mode: state file overrides config
	mode := cfg.InitialMode
	if cfg.StateManager != nil {
		if stateMode := cfg.StateManager.GetPrivacyMode(); stateMode != "" {
			mode = stateMode
			cfg.Logger.Info("using privacy mode from state file",
				zap.String("mode", mode),
			)
		}
	}

	if mode == "" {
		mode = "strict"
	}
	if mode != "strict" && mode != "passthrough" {
		return nil, fmt.Errorf("invalid mode: %s", mode)
	}

	a := &Agent{
		id:             cfg.AgentID,
		organizationID: cfg.OrganizationID,
		version:        cfg.Version,
		startTime:      time.Now().UTC(),
		stateManager:   cfg.StateManager,
		logger:         cfg.Logger,
	}
	a.mode.Store(mode)

	return a, nil
}

// ID returns the agent ID
func (a *Agent) ID() string {
	return a.id
}

// OrganizationID returns the organization ID
func (a *Agent) OrganizationID() string {
	return a.organizationID
}

// Version returns the agent version
func (a *Agent) Version() string {
	return a.version
}

// Uptime returns how long the agent has been running
func (a *Agent) Uptime() time.Duration {
	return time.Since(a.startTime)
}

// GetMode returns the current privacy mode
func (a *Agent) GetMode() string {
	return a.mode.Load().(string)
}

// SwitchMode switches between strict and passthrough privacy modes
// and persists the change to the state file
func (a *Agent) SwitchMode(mode string) error {
	if mode != "strict" && mode != "passthrough" {
		return fmt.Errorf("invalid mode: %s (must be 'strict' or 'passthrough')", mode)
	}

	oldMode := a.mode.Load().(string)
	a.mode.Store(mode)

	// Persist mode change to state file
	if a.stateManager != nil {
		if err := a.stateManager.SetPrivacyMode(mode); err != nil {
			// Rollback in-memory mode on persistence failure
			a.mode.Store(oldMode)
			return fmt.Errorf("failed to persist mode change: %w", err)
		}
	}

	a.logger.Info("privacy mode switched",
		zap.String("from", oldMode),
		zap.String("to", mode),
	)

	return nil
}

// SetShutdownFunc registers a function to call during Stop/Restart
func (a *Agent) SetShutdownFunc(fn context.CancelFunc) {
	a.shutdownMu.Lock()
	defer a.shutdownMu.Unlock()
	a.shutdownFn = fn
}

// Stop gracefully shuts down the agent
func (a *Agent) Stop(ctx context.Context) error {
	a.shutdownMu.Lock()
	fn := a.shutdownFn
	a.shutdownMu.Unlock()

	if fn == nil {
		return fmt.Errorf("no shutdown function registered")
	}

	a.logger.Info("stopping agent", zap.String("agent_id", a.id))

	// Trigger shutdown
	fn()

	return nil
}

// Restart performs a graceful restart
//
// Note: In containerized environments, restart is typically handled by
// the orchestrator (Docker, Kubernetes) by exiting the process.
func (a *Agent) Restart(ctx context.Context) error {
	a.logger.Info("restarting agent", zap.String("agent_id", a.id))

	// For now, restart is the same as stop - the orchestrator will restart
	return a.Stop(ctx)
}
