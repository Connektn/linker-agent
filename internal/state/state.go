// Package state provides persistent state management for the Connektn Linker Agent.
// It handles runtime configuration changes that must survive agent restarts,
// such as privacy mode switches triggered by control commands.
//
// Security: State file contains no PII or secrets, only operational mode settings.
// File permissions are set to 0600 (owner read/write only).
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// AgentState represents the persisted runtime state of the agent
type AgentState struct {
	// PrivacyMode is the current privacy mode ("strict" or "passthrough")
	// This overrides the default mode from config.yaml when set
	PrivacyMode string `json:"privacyMode,omitempty"`
}

// Manager handles loading and saving agent state
type Manager struct {
	filePath string
	state    AgentState
	mu       sync.RWMutex
}

// NewManager creates a new state manager
// filePath: path to state file (e.g., "/var/lib/connektn/agent-state.json")
func NewManager(filePath string) (*Manager, error) {
	if filePath == "" {
		return nil, fmt.Errorf("state file path is required")
	}

	m := &Manager{
		filePath: filePath,
	}

	// Create parent directory if it doesn't exist
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	// Load existing state (if any)
	if err := m.Load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load state: %w", err)
	}

	return m, nil
}

// Load reads the state from disk
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// No state file yet - this is normal for first run
			m.state = AgentState{}
			return nil
		}
		return err
	}

	if err := json.Unmarshal(data, &m.state); err != nil {
		return fmt.Errorf("failed to parse state file: %w", err)
	}

	return nil
}

// Save writes the current state to disk atomically
func (m *Manager) Save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Write to temp file first, then rename (atomic operation)
	tempPath := m.filePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write temp state file: %w", err)
	}

	if err := os.Rename(tempPath, m.filePath); err != nil {
		os.Remove(tempPath) // Clean up temp file
		return fmt.Errorf("failed to rename state file: %w", err)
	}

	return nil
}

// GetPrivacyMode returns the current privacy mode from state
// Returns empty string if not set (use config default in this case)
func (m *Manager) GetPrivacyMode() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state.PrivacyMode
}

// SetPrivacyMode updates the privacy mode and persists to disk
func (m *Manager) SetPrivacyMode(mode string) error {
	if mode != "" && mode != "strict" && mode != "passthrough" {
		return fmt.Errorf("invalid privacy mode: %s (must be 'strict' or 'passthrough')", mode)
	}

	m.mu.Lock()
	m.state.PrivacyMode = mode
	m.mu.Unlock()

	return m.Save()
}

// GetState returns a copy of the current state
func (m *Manager) GetState() AgentState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}
