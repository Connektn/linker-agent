package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateManager_CreateAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "agent-state.json")

	// Create new manager
	mgr, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Initially should be empty
	if mode := mgr.GetPrivacyMode(); mode != "" {
		t.Errorf("Expected empty mode, got %s", mode)
	}
}

func TestStateManager_SetAndGetPrivacyMode(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "agent-state.json")

	mgr, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Set mode to strict
	if err := mgr.SetPrivacyMode("strict"); err != nil {
		t.Fatalf("Failed to set mode: %v", err)
	}

	// Verify it was set
	if mode := mgr.GetPrivacyMode(); mode != "strict" {
		t.Errorf("Expected mode 'strict', got %s", mode)
	}

	// Verify file was created
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		t.Error("State file was not created")
	}
}

func TestStateManager_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "agent-state.json")

	// Create manager and set mode
	mgr1, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	if err := mgr1.SetPrivacyMode("passthrough"); err != nil {
		t.Fatalf("Failed to set mode: %v", err)
	}

	// Create new manager (simulates restart)
	mgr2, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("Failed to create second manager: %v", err)
	}

	// Verify mode persisted
	if mode := mgr2.GetPrivacyMode(); mode != "passthrough" {
		t.Errorf("Expected mode 'passthrough' after reload, got %s", mode)
	}
}

func TestStateManager_InvalidMode(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "agent-state.json")

	mgr, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Try to set invalid mode
	err = mgr.SetPrivacyMode("invalid")
	if err == nil {
		t.Error("Expected error for invalid mode, got nil")
	}
}

func TestStateManager_FilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "agent-state.json")

	mgr, err := NewManager(statePath)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	if err := mgr.SetPrivacyMode("strict"); err != nil {
		t.Fatalf("Failed to set mode: %v", err)
	}

	// Check file permissions
	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("Expected file permissions 0600, got %o", perm)
	}
}
