package polling

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestPoller_Poll_NoContent(t *testing.T) {
	// Create test server that returns 204 No Content
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request structure
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST request, got %s", r.Method)
		}
		if r.URL.Path != "/api/agent/command/poll" {
			t.Errorf("Expected /api/agent/command/poll, got %s", r.URL.Path)
		}

		// Return 204 No Content
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	// Create poller
	poller, err := NewPoller(PollerConfig{
		CDPBaseURL:     server.URL,
		AgentID:        "test-agent",
		OrganizationID: "test-org",
		Secret:         "dGVzdC1zZWNyZXQ=", // base64("test-secret")
		Interval:       30 * time.Second,
		Timeout:        10 * time.Second,
		Logger:         zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("Failed to create poller: %v", err)
	}

	// Test poll
	ctx := context.Background()
	cmd, err := poller.poll(ctx)

	// Should return nil command with no error
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if cmd != nil {
		t.Errorf("Expected nil command for 204 response, got: %+v", cmd)
	}
}

func TestPoller_Poll_EmptyJSON(t *testing.T) {
	// Create test server that returns 200 OK with empty JSON body
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request structure
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST request, got %s", r.Method)
		}
		if r.URL.Path != "/api/agent/command/poll" {
			t.Errorf("Expected /api/agent/command/poll, got %s", r.URL.Path)
		}

		// Return 200 OK with empty JSON
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer server.Close()

	// Create poller
	poller, err := NewPoller(PollerConfig{
		CDPBaseURL:     server.URL,
		AgentID:        "test-agent",
		OrganizationID: "test-org",
		Secret:         "dGVzdC1zZWNyZXQ=", // base64("test-secret")
		Interval:       30 * time.Second,
		Timeout:        10 * time.Second,
		Logger:         zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("Failed to create poller: %v", err)
	}

	// Test poll
	ctx := context.Background()
	cmd, err := poller.poll(ctx)

	// Should return nil command with no error (treat empty JSON as no command)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if cmd != nil {
		t.Errorf("Expected nil command for empty JSON response, got: %+v", cmd)
	}
}

func TestPoller_Poll_ValidCommand(t *testing.T) {
	// Create test server that returns a valid command
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request structure
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST request, got %s", r.Method)
		}
		if r.URL.Path != "/api/agent/command/poll" {
			t.Errorf("Expected /api/agent/command/poll, got %s", r.URL.Path)
		}

		// Verify headers
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type: application/json")
		}
		if r.Header.Get("X-Timestamp") == "" {
			t.Errorf("Expected X-Timestamp header")
		}
		if r.Header.Get("X-Nonce") == "" {
			t.Errorf("Expected X-Nonce header")
		}
		if r.Header.Get("X-Signature") == "" {
			t.Errorf("Expected X-Signature header")
		}

		// Return valid command
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(Command{
			CommandID: "cmd-123",
			Command:   CommandSwitchMode,
			Params: map[string]interface{}{
				"mode": "passthrough",
			},
		})
	}))
	defer server.Close()

	// Create poller
	poller, err := NewPoller(PollerConfig{
		CDPBaseURL:     server.URL,
		AgentID:        "test-agent",
		OrganizationID: "test-org",
		Secret:         "dGVzdC1zZWNyZXQ=", // base64("test-secret")
		Interval:       30 * time.Second,
		Timeout:        10 * time.Second,
		Logger:         zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("Failed to create poller: %v", err)
	}

	// Test poll
	ctx := context.Background()
	cmd, err := poller.poll(ctx)

	// Should return command
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if cmd == nil {
		t.Fatalf("Expected command, got nil")
	}
	if cmd.CommandID != "cmd-123" {
		t.Errorf("Expected CommandID 'cmd-123', got '%s'", cmd.CommandID)
	}
	if cmd.Command != CommandSwitchMode {
		t.Errorf("Expected Command 'switch_mode', got '%s'", cmd.Command)
	}
	if mode, ok := cmd.Params["mode"].(string); !ok || mode != "passthrough" {
		t.Errorf("Expected mode 'passthrough', got %v", cmd.Params["mode"])
	}
}

func TestPoller_Poll_ErrorResponse(t *testing.T) {
	// Create test server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal server error"}`))
	}))
	defer server.Close()

	// Create poller
	poller, err := NewPoller(PollerConfig{
		CDPBaseURL:     server.URL,
		AgentID:        "test-agent",
		OrganizationID: "test-org",
		Secret:         "dGVzdC1zZWNyZXQ=", // base64("test-secret")
		Interval:       30 * time.Second,
		Timeout:        10 * time.Second,
		Logger:         zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("Failed to create poller: %v", err)
	}

	// Test poll
	ctx := context.Background()
	cmd, err := poller.poll(ctx)

	// Should return error
	if err == nil {
		t.Errorf("Expected error for 500 response, got nil")
	}
	if cmd != nil {
		t.Errorf("Expected nil command for error response, got: %+v", cmd)
	}
}

func TestPoller_Poll_RequestBody(t *testing.T) {
	// Create test server to verify request body
	var receivedBody PollRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Decode request body
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
		}

		// Return 204
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	// Create poller
	poller, err := NewPoller(PollerConfig{
		CDPBaseURL:     server.URL,
		AgentID:        "test-agent-456",
		OrganizationID: "test-org-789",
		Secret:         "dGVzdC1zZWNyZXQ=", // base64("test-secret")
		Interval:       30 * time.Second,
		Timeout:        10 * time.Second,
		Logger:         zap.NewNop(),
	})
	if err != nil {
		t.Fatalf("Failed to create poller: %v", err)
	}

	// Test poll
	ctx := context.Background()
	_, err = poller.poll(ctx)
	if err != nil {
		t.Fatalf("Poll failed: %v", err)
	}

	// Verify request body
	if receivedBody.AgentID != "test-agent-456" {
		t.Errorf("Expected AgentID 'test-agent-456', got '%s'", receivedBody.AgentID)
	}
	if receivedBody.OrganizationID != "test-org-789" {
		t.Errorf("Expected OrganizationID 'test-org-789', got '%s'", receivedBody.OrganizationID)
	}
}

func TestNewPoller_Validation(t *testing.T) {
	tests := []struct {
		name    string
		config  PollerConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: PollerConfig{
				CDPBaseURL:     "http://localhost:8080",
				AgentID:        "test-agent",
				OrganizationID: "test-org",
				Secret:         "dGVzdC1zZWNyZXQ=",
				Interval:       30 * time.Second,
				Timeout:        10 * time.Second,
				Logger:         zap.NewNop(),
			},
			wantErr: false,
		},
		{
			name: "missing base URL",
			config: PollerConfig{
				AgentID:        "test-agent",
				OrganizationID: "test-org",
				Secret:         "dGVzdC1zZWNyZXQ=",
				Interval:       30 * time.Second,
				Timeout:        10 * time.Second,
				Logger:         zap.NewNop(),
			},
			wantErr: true,
		},
		{
			name: "missing agent ID",
			config: PollerConfig{
				CDPBaseURL:     "http://localhost:8080",
				OrganizationID: "test-org",
				Secret:         "dGVzdC1zZWNyZXQ=",
				Interval:       30 * time.Second,
				Timeout:        10 * time.Second,
				Logger:         zap.NewNop(),
			},
			wantErr: true,
		},
		{
			name: "missing organization ID",
			config: PollerConfig{
				CDPBaseURL: "http://localhost:8080",
				AgentID:    "test-agent",
				Secret:     "dGVzdC1zZWNyZXQ=",
				Interval:   30 * time.Second,
				Timeout:    10 * time.Second,
				Logger:     zap.NewNop(),
			},
			wantErr: true,
		},
		{
			name: "missing secret",
			config: PollerConfig{
				CDPBaseURL:     "http://localhost:8080",
				AgentID:        "test-agent",
				OrganizationID: "test-org",
				Interval:       30 * time.Second,
				Timeout:        10 * time.Second,
				Logger:         zap.NewNop(),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPoller(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewPoller() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
