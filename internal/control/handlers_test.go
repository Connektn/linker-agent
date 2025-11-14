package control

import (
	"context"
	"fmt"
	"testing"

	"go.uber.org/zap"
)

// mockAgentController is a mock implementation of AgentController for testing
type mockAgentController struct {
	stopCalled    bool
	restartCalled bool
	mode          string
	stopErr       error
	restartErr    error
	switchModeErr error
}

func (m *mockAgentController) Stop(ctx context.Context) error {
	m.stopCalled = true
	return m.stopErr
}

func (m *mockAgentController) Restart(ctx context.Context) error {
	m.restartCalled = true
	return m.restartErr
}

func (m *mockAgentController) SwitchMode(mode string) error {
	if m.switchModeErr != nil {
		return m.switchModeErr
	}
	m.mode = mode
	return nil
}

func (m *mockAgentController) GetMode() string {
	return m.mode
}

func TestHandlers_HandleStop_Success(t *testing.T) {
	ctrl := &mockAgentController{}
	h := NewHandlers(ctrl, zap.NewNop())

	err := h.HandleStop(nil)
	if err != nil {
		t.Errorf("HandleStop failed: %v", err)
	}

	if !ctrl.stopCalled {
		t.Error("Stop was not called on controller")
	}
}

func TestHandlers_HandleStop_Error(t *testing.T) {
	ctrl := &mockAgentController{
		stopErr: fmt.Errorf("stop failed"),
	}
	h := NewHandlers(ctrl, zap.NewNop())

	err := h.HandleStop(nil)
	if err == nil {
		t.Error("HandleStop should have failed")
	}

	if !ctrl.stopCalled {
		t.Error("Stop was not called on controller")
	}
}

func TestHandlers_HandleStop_AlreadyInProgress(t *testing.T) {
	ctrl := &mockAgentController{}
	h := NewHandlers(ctrl, zap.NewNop())

	// Set shutdown in progress
	h.shutdownInProgress.Store(true)

	err := h.HandleStop(nil)
	if err == nil {
		t.Error("HandleStop should fail when shutdown is already in progress")
	}

	if ctrl.stopCalled {
		t.Error("Stop should not be called when shutdown is already in progress")
	}
}

func TestHandlers_HandleStart(t *testing.T) {
	ctrl := &mockAgentController{}
	h := NewHandlers(ctrl, zap.NewNop())

	// Start is currently a no-op
	err := h.HandleStart(nil)
	if err != nil {
		t.Errorf("HandleStart failed: %v", err)
	}
}

func TestHandlers_HandleRestart_Success(t *testing.T) {
	ctrl := &mockAgentController{}
	h := NewHandlers(ctrl, zap.NewNop())

	err := h.HandleRestart(nil)
	if err != nil {
		t.Errorf("HandleRestart failed: %v", err)
	}

	if !ctrl.restartCalled {
		t.Error("Restart was not called on controller")
	}
}

func TestHandlers_HandleRestart_WithMode(t *testing.T) {
	ctrl := &mockAgentController{}
	h := NewHandlers(ctrl, zap.NewNop())

	params := map[string]interface{}{
		"mode": "immediate",
	}

	err := h.HandleRestart(params)
	if err != nil {
		t.Errorf("HandleRestart failed: %v", err)
	}

	if !ctrl.restartCalled {
		t.Error("Restart was not called on controller")
	}
}

func TestHandlers_HandleRestart_Error(t *testing.T) {
	ctrl := &mockAgentController{
		restartErr: fmt.Errorf("restart failed"),
	}
	h := NewHandlers(ctrl, zap.NewNop())

	err := h.HandleRestart(nil)
	if err == nil {
		t.Error("HandleRestart should have failed")
	}

	if !ctrl.restartCalled {
		t.Error("Restart was not called on controller")
	}
}

func TestHandlers_HandleSwitchMode_Success(t *testing.T) {
	ctrl := &mockAgentController{
		mode: "strict",
	}
	h := NewHandlers(ctrl, zap.NewNop())

	params := map[string]interface{}{
		"mode": "passthrough",
	}

	err := h.HandleSwitchMode(params)
	if err != nil {
		t.Errorf("HandleSwitchMode failed: %v", err)
	}

	if ctrl.mode != "passthrough" {
		t.Errorf("Mode = %s, want passthrough", ctrl.mode)
	}
}

func TestHandlers_HandleSwitchMode_MissingParam(t *testing.T) {
	ctrl := &mockAgentController{}
	h := NewHandlers(ctrl, zap.NewNop())

	err := h.HandleSwitchMode(nil)
	if err == nil {
		t.Error("HandleSwitchMode should fail without mode parameter")
	}
}

func TestHandlers_HandleSwitchMode_InvalidMode(t *testing.T) {
	ctrl := &mockAgentController{}
	h := NewHandlers(ctrl, zap.NewNop())

	params := map[string]interface{}{
		"mode": "invalid",
	}

	err := h.HandleSwitchMode(params)
	if err == nil {
		t.Error("HandleSwitchMode should fail with invalid mode")
	}
}

func TestHandlers_HandleSwitchMode_AlreadySet(t *testing.T) {
	ctrl := &mockAgentController{
		mode: "strict",
	}
	h := NewHandlers(ctrl, zap.NewNop())

	params := map[string]interface{}{
		"mode": "strict",
	}

	// Should succeed but not call SwitchMode
	err := h.HandleSwitchMode(params)
	if err != nil {
		t.Errorf("HandleSwitchMode failed: %v", err)
	}

	if ctrl.mode != "strict" {
		t.Error("Mode should remain unchanged")
	}
}

func TestHandlers_HandleUpgrade_MissingVersion(t *testing.T) {
	ctrl := &mockAgentController{}
	h := NewHandlers(ctrl, zap.NewNop())

	err := h.HandleUpgrade(nil)
	if err == nil {
		t.Error("HandleUpgrade should fail without version parameter")
	}
}

func TestHandlers_HandleUpgrade_NotImplemented(t *testing.T) {
	ctrl := &mockAgentController{}
	h := NewHandlers(ctrl, zap.NewNop())

	params := map[string]interface{}{
		"version": "2.0.0",
	}

	err := h.HandleUpgrade(params)
	if err == nil {
		t.Error("HandleUpgrade should return not implemented error")
	}
}

func TestHandlers_RegisterAll(t *testing.T) {
	ctrl := &mockAgentController{}
	h := NewHandlers(ctrl, zap.NewNop())

	handler := NewHandler("test-secret", 0, 0)
	h.RegisterAll(handler)

	// Check that all commands are registered
	expectedCommands := []CommandType{
		CommandStop,
		CommandStart,
		CommandRestart,
		CommandSwitchMode,
		CommandUpgrade,
	}

	handler.mu.RLock()
	defer handler.mu.RUnlock()

	for _, cmd := range expectedCommands {
		if _, exists := handler.commandRouter[cmd]; !exists {
			t.Errorf("Command %s not registered", cmd)
		}
	}

	if len(handler.commandRouter) != len(expectedCommands) {
		t.Errorf("Expected %d commands, got %d", len(expectedCommands), len(handler.commandRouter))
	}
}
