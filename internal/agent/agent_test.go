package agent

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestNew_Success(t *testing.T) {
	cfg := Config{
		AgentID:        "agent_test123",
		OrganizationID: "org_456",
		Version:        "1.0.0",
		InitialMode:    "strict",
		Logger:         zap.NewNop(),
	}

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if a.ID() != "agent_test123" {
		t.Errorf("ID = %s, want agent_test123", a.ID())
	}

	if a.OrganizationID() != "org_456" {
		t.Errorf("OrganizationID = %s, want org_456", a.OrganizationID())
	}

	if a.Version() != "1.0.0" {
		t.Errorf("Version = %s, want 1.0.0", a.Version())
	}

	if a.GetMode() != "strict" {
		t.Errorf("GetMode = %s, want strict", a.GetMode())
	}
}

func TestNew_Defaults(t *testing.T) {
	cfg := Config{
		AgentID:        "agent_test123",
		OrganizationID: "org_456",
		// Version and InitialMode not set
	}

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if a.Version() != "dev" {
		t.Errorf("Version = %s, want dev", a.Version())
	}

	if a.GetMode() != "strict" {
		t.Errorf("GetMode = %s, want strict (default)", a.GetMode())
	}
}

func TestNew_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "missing agent ID",
			cfg: Config{
				OrganizationID: "org_456",
			},
		},
		{
			name: "missing organization ID",
			cfg: Config{
				AgentID: "agent_test123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.cfg)
			if err == nil {
				t.Error("New should fail with missing required field")
			}
		})
	}
}

func TestNew_InvalidMode(t *testing.T) {
	cfg := Config{
		AgentID:        "agent_test123",
		OrganizationID: "org_456",
		InitialMode:    "invalid",
	}

	_, err := New(cfg)
	if err == nil {
		t.Error("New should fail with invalid mode")
	}
}

func TestAgent_Uptime(t *testing.T) {
	cfg := Config{
		AgentID:        "agent_test123",
		OrganizationID: "org_456",
	}

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	uptime := a.Uptime()
	if uptime < 100*time.Millisecond {
		t.Errorf("Uptime = %v, want >= 100ms", uptime)
	}
}

func TestAgent_SwitchMode(t *testing.T) {
	cfg := Config{
		AgentID:        "agent_test123",
		OrganizationID: "org_456",
		InitialMode:    "strict",
		Logger:         zap.NewNop(),
	}

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// Switch to passthrough
	if err := a.SwitchMode("passthrough"); err != nil {
		t.Errorf("SwitchMode failed: %v", err)
	}

	if a.GetMode() != "passthrough" {
		t.Errorf("GetMode = %s, want passthrough", a.GetMode())
	}

	// Switch back to strict
	if err := a.SwitchMode("strict"); err != nil {
		t.Errorf("SwitchMode failed: %v", err)
	}

	if a.GetMode() != "strict" {
		t.Errorf("GetMode = %s, want strict", a.GetMode())
	}

	// Try invalid mode
	if err := a.SwitchMode("invalid"); err == nil {
		t.Error("SwitchMode should fail with invalid mode")
	}
}

func TestAgent_Stop(t *testing.T) {
	cfg := Config{
		AgentID:        "agent_test123",
		OrganizationID: "org_456",
		Logger:         zap.NewNop(),
	}

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// Stop should fail without shutdown function
	if err := a.Stop(context.Background()); err == nil {
		t.Error("Stop should fail without shutdown function")
	}

	// Register shutdown function
	shutdownCalled := false
	ctx, cancel := context.WithCancel(context.Background())
	a.SetShutdownFunc(cancel)

	go func() {
		<-ctx.Done()
		shutdownCalled = true
	}()

	// Stop should succeed
	if err := a.Stop(context.Background()); err != nil {
		t.Errorf("Stop failed: %v", err)
	}

	// Give it time to propagate
	time.Sleep(10 * time.Millisecond)

	if !shutdownCalled {
		t.Error("Shutdown function was not called")
	}
}

func TestAgent_Restart(t *testing.T) {
	cfg := Config{
		AgentID:        "agent_test123",
		OrganizationID: "org_456",
		Logger:         zap.NewNop(),
	}

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// Register shutdown function
	shutdownCalled := false
	ctx, cancel := context.WithCancel(context.Background())
	a.SetShutdownFunc(cancel)

	go func() {
		<-ctx.Done()
		shutdownCalled = true
	}()

	// Restart should call shutdown
	if err := a.Restart(context.Background()); err != nil {
		t.Errorf("Restart failed: %v", err)
	}

	// Give it time to propagate
	time.Sleep(10 * time.Millisecond)

	if !shutdownCalled {
		t.Error("Shutdown function was not called during restart")
	}
}
