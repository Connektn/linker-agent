package exporter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"linker-agent/internal/config"
)

// TestPerStreamRouting verifies that different streams route to different endpoints/files.
func TestPerStreamRouting(t *testing.T) {
	t.Run("file mode with per-stream paths", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg := config.Export{
			Mode: "file",
			File: config.ExportFile{
				Paths: map[string]string{
					"edges":   filepath.Join(tmpDir, "edges.jsonl"),
					"billing": filepath.Join(tmpDir, "billing.jsonl"),
				},
			},
		}

		exp, err := New(Options{
			Config:        cfg,
			TenantKey:     "test-key",
			QueueCapacity: 100,
		})
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}

		exp.Start(context.Background())

		ctx := context.Background()

		// Enqueue to different streams
		_ = exp.Enqueue(ctx, StreamEdges, map[string]string{"type": "edge", "id": "1"})
		_ = exp.Enqueue(ctx, StreamEdges, map[string]string{"type": "edge", "id": "2"})
		_ = exp.Enqueue(ctx, StreamBilling, map[string]string{"type": "billing", "id": "3"})

		// Wait for flush
		time.Sleep(200 * time.Millisecond)
		exp.Shutdown(context.Background())

		// Verify edges file
		edgesData, err := os.ReadFile(cfg.File.Paths["edges"])
		if err != nil {
			t.Fatalf("Failed to read edges file: %v", err)
		}
		var edgesArray []map[string]string
		if err := json.Unmarshal(edgesData[:len(edgesData)-1], &edgesArray); err != nil {
			t.Fatalf("Edges file has invalid JSON: %v", err)
		}
		if len(edgesArray) != 2 {
			t.Errorf("Expected 2 edges, got %d", len(edgesArray))
		}

		// Verify billing file
		billingData, err := os.ReadFile(cfg.File.Paths["billing"])
		if err != nil {
			t.Fatalf("Failed to read billing file: %v", err)
		}
		var billingArray []map[string]string
		if err := json.Unmarshal(billingData[:len(billingData)-1], &billingArray); err != nil {
			t.Fatalf("Billing file has invalid JSON: %v", err)
		}
		if len(billingArray) != 1 {
			t.Errorf("Expected 1 billing record, got %d", len(billingArray))
		}
	})

	t.Run("http mode with per-stream routes", func(t *testing.T) {
		edgesRequests := int32(0)
		billingRequests := int32(0)

		mux := http.NewServeMux()
		mux.HandleFunc("/v1/edges", func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&edgesRequests, 1)
			w.WriteHeader(http.StatusOK)
		})
		mux.HandleFunc("/v1/billing", func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&billingRequests, 1)
			w.WriteHeader(http.StatusOK)
		})

		server := httptest.NewServer(mux)
		defer server.Close()

		cfg := config.Export{
			Mode: "http",
			HTTP: config.ExportHTTP{
				BaseURL: server.URL,
				Routes: map[string]string{
					"edges":   "/v1/edges",
					"billing": "/v1/billing",
				},
				BatchSize:  10,
				MaxRetries: 3,
				FlushEvery: 100 * time.Millisecond,
			},
		}

		exp, err := New(Options{
			Config:    cfg,
			TenantKey: "test-key",
		})
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}

		exp.Start(context.Background())

		ctx := context.Background()

		// Enqueue to different streams
		_ = exp.Enqueue(ctx, StreamEdges, map[string]string{"type": "edge"})
		_ = exp.Enqueue(ctx, StreamBilling, map[string]string{"type": "billing"})

		// Wait for flush
		time.Sleep(200 * time.Millisecond)
		exp.Shutdown(context.Background())

		// Verify routes received correct requests
		if count := atomic.LoadInt32(&edgesRequests); count != 1 {
			t.Errorf("Expected 1 request to /v1/edges, got %d", count)
		}
		if count := atomic.LoadInt32(&billingRequests); count != 1 {
			t.Errorf("Expected 1 request to /v1/billing, got %d", count)
		}
	})

	t.Run("both mode with per-stream configuration", func(t *testing.T) {
		httpRequests := int32(0)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&httpRequests, 1)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		tmpDir := t.TempDir()

		cfg := config.Export{
			Mode: "both",
			HTTP: config.ExportHTTP{
				BaseURL: server.URL,
				Routes: map[string]string{
					"edges": "/v1/edges",
				},
				BatchSize:  10,
				MaxRetries: 3,
				FlushEvery: 100 * time.Millisecond,
			},
			File: config.ExportFile{
				Paths: map[string]string{
					"edges": filepath.Join(tmpDir, "edges.jsonl"),
				},
			},
		}

		exp, err := New(Options{
			Config:    cfg,
			TenantKey: "test-key",
		})
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}

		exp.Start(context.Background())

		ctx := context.Background()
		_ = exp.Enqueue(ctx, StreamEdges, map[string]string{"id": "1"})

		// Wait for flush
		time.Sleep(200 * time.Millisecond)
		exp.Shutdown(context.Background())

		// Verify HTTP received request
		if count := atomic.LoadInt32(&httpRequests); count != 1 {
			t.Errorf("Expected 1 HTTP request, got %d", count)
		}

		// Verify file was written
		if _, err := os.Stat(cfg.File.Paths["edges"]); os.IsNotExist(err) {
			t.Error("Expected edges file to be created")
		}
	})
}

// TestStreamValidation verifies that stream configuration is validated at enqueue time.
func TestStreamValidation(t *testing.T) {
	t.Run("missing HTTP route", func(t *testing.T) {
		cfg := config.Export{
			Mode: "http",
			HTTP: config.ExportHTTP{
				BaseURL: "https://api.example.com",
				Routes: map[string]string{
					"edges": "/v1/edges",
					// "billing" route missing
				},
				BatchSize:  10,
				MaxRetries: 3,
				FlushEvery: 100 * time.Millisecond,
			},
		}

		exp, err := New(Options{
			Config:    cfg,
			TenantKey: "test-key",
		})
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}

		ctx := context.Background()

		// Enqueue to configured stream should succeed
		if err := exp.Enqueue(ctx, StreamEdges, map[string]string{"id": "1"}); err != nil {
			t.Errorf("Enqueue to configured stream failed: %v", err)
		}

		// Enqueue to unconfigured stream should fail
		if err := exp.Enqueue(ctx, StreamBilling, map[string]string{"id": "2"}); err == nil {
			t.Error("Expected error when enqueuing to unconfigured stream")
		}
	})

	t.Run("missing file path", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg := config.Export{
			Mode: "file",
			File: config.ExportFile{
				Paths: map[string]string{
					"edges": filepath.Join(tmpDir, "edges.jsonl"),
					// "billing" path missing
				},
			},
		}

		exp, err := New(Options{
			Config: cfg,
		})
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}

		ctx := context.Background()

		// Enqueue to configured stream should succeed
		if err := exp.Enqueue(ctx, StreamEdges, map[string]string{"id": "1"}); err != nil {
			t.Errorf("Enqueue to configured stream failed: %v", err)
		}

		// Enqueue to unconfigured stream should fail
		if err := exp.Enqueue(ctx, StreamBilling, map[string]string{"id": "2"}); err == nil {
			t.Error("Expected error when enqueuing to unconfigured stream")
		}
	})

	t.Run("undefined stream", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg := config.Export{
			Mode: "file",
			File: config.ExportFile{
				Paths: map[string]string{
					"edges": filepath.Join(tmpDir, "edges.jsonl"),
				},
			},
		}

		exp, err := New(Options{
			Config: cfg,
		})
		if err != nil {
			t.Fatalf("New() failed: %v", err)
		}

		ctx := context.Background()

		// Enqueue to undefined stream should fail
		if err := exp.Enqueue(ctx, Stream("nonexistent"), map[string]string{"id": "1"}); err == nil {
			t.Error("Expected error when enqueuing to undefined stream")
		}
	})
}

// TestHTTPAuthorizationFromEnv verifies that authorization tokens can be read from environment.
func TestHTTPAuthorizationFromEnv(t *testing.T) {
	receivedAuth := ""
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedAuth = r.Header.Get("Authorization")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Set environment variable
	envKey := "TEST_AUTH_TOKEN"
	envValue := "token-from-env-12345"
	os.Setenv(envKey, envValue)
	defer os.Unsetenv(envKey)

	cfg := config.Export{
		Mode: "http",
		HTTP: config.ExportHTTP{
			BaseURL: server.URL,
			Routes: map[string]string{
				"edges": "/v1/edges",
			},
			Headers: config.ExportHTTPHeaders{
				AuthorizationEnv: envKey,
			},
			BatchSize:  10,
			MaxRetries: 3,
			FlushEvery: 100 * time.Millisecond,
		},
	}

	exp, err := New(Options{
		Config:    cfg,
		TenantKey: "default-key", // Should be overridden by env
	})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	exp.Start(context.Background())

	ctx := context.Background()
	_ = exp.Enqueue(ctx, StreamEdges, map[string]string{"id": "1"})

	// Wait for flush
	time.Sleep(200 * time.Millisecond)
	exp.Shutdown(context.Background())

	// Verify authorization header came from environment
	mu.Lock()
	expectedAuth := "Bearer " + envValue
	if receivedAuth != expectedAuth {
		t.Errorf("Expected Authorization header %q, got %q", expectedAuth, receivedAuth)
	}
	mu.Unlock()
}

// TestValidatePassthroughExportOptions verifies passthrough mode guardrails.
func TestValidatePassthroughExportOptions(t *testing.T) {
	tests := []struct {
		name             string
		idMode           string
		allowPassthrough bool
		exportMode       string
		baseURL          string
		wantErr          bool
		errContains      string
	}{
		{
			name:             "strict mode always allowed",
			idMode:           "strict",
			allowPassthrough: false,
			exportMode:       "http",
			baseURL:          "https://example.com",
			wantErr:          false,
		},
		{
			name:             "passthrough to Connektn dev endpoint",
			idMode:           "passthrough",
			allowPassthrough: false,
			exportMode:       "http",
			baseURL:          "https://api.connektn.dev",
			wantErr:          false,
		},
		{
			name:             "passthrough to Connektn io endpoint",
			idMode:           "passthrough",
			allowPassthrough: false,
			exportMode:       "http",
			baseURL:          "https://api.connektn.io",
			wantErr:          false,
		},
		{
			name:             "passthrough to Connektn endpoint with trailing slash",
			idMode:           "passthrough",
			allowPassthrough: false,
			exportMode:       "http",
			baseURL:          "https://api.connektn.dev/",
			wantErr:          false,
		},
		{
			name:             "passthrough to non-Connektn endpoint without allow flag",
			idMode:           "passthrough",
			allowPassthrough: false,
			exportMode:       "http",
			baseURL:          "https://example.com",
			wantErr:          true,
			errContains:      "allowPassthroughExports = true",
		},
		{
			name:             "passthrough to non-Connektn endpoint with allow flag",
			idMode:           "passthrough",
			allowPassthrough: true,
			exportMode:       "http",
			baseURL:          "https://example.com",
			wantErr:          false,
		},
		{
			name:             "passthrough file mode always allowed",
			idMode:           "passthrough",
			allowPassthrough: false,
			exportMode:       "file",
			baseURL:          "",
			wantErr:          false,
		},
		{
			name:             "passthrough both mode to non-Connektn without allow",
			idMode:           "passthrough",
			allowPassthrough: false,
			exportMode:       "both",
			baseURL:          "https://example.com",
			wantErr:          true,
			errContains:      "allowPassthroughExports = true",
		},
		{
			name:             "passthrough both mode to Connektn endpoint",
			idMode:           "passthrough",
			allowPassthrough: false,
			exportMode:       "both",
			baseURL:          "https://api.connektn.io",
			wantErr:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassthroughExportOptions(tt.idMode, tt.allowPassthrough, tt.exportMode, tt.baseURL)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidatePassthroughExportOptions() error = nil, wantErr = true")
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("ValidatePassthroughExportOptions() error = %q, want to contain %q", err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("ValidatePassthroughExportOptions() unexpected error = %v", err)
				}
			}
		})
	}
}

// contains is a helper function to check if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
