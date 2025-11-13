package control

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// Server provides the HTTP API for receiving control commands from Connektn Cloud
type Server struct {
	handler    *Handler
	httpServer *http.Server
	logger     *zap.Logger
}

// ServerConfig holds configuration for the control server
type ServerConfig struct {
	ListenAddr      string
	SignatureSecret string
	MaxClockSkew    time.Duration
	NonceCacheTTL   time.Duration
	Logger          *zap.Logger
}

// NewServer creates a new control command HTTP server
func NewServer(cfg ServerConfig) *Server {
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}

	handler := NewHandler(cfg.SignatureSecret, cfg.MaxClockSkew, cfg.NonceCacheTTL)

	mux := http.NewServeMux()
	srv := &Server{
		handler: handler,
		httpServer: &http.Server{
			Addr:         cfg.ListenAddr,
			Handler:      mux,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		logger: cfg.Logger,
	}

	// Register routes
	mux.HandleFunc("/api/control/command", srv.handleCommand)
	mux.HandleFunc("/healthz", srv.handleHealth)

	return srv
}

// RegisterCommand registers a handler function for a specific command type
func (s *Server) RegisterCommand(cmdType CommandType, fn CommandFunc) {
	s.handler.RegisterCommand(cmdType, fn)
}

// Start starts the HTTP server
func (s *Server) Start() error {
	s.logger.Info("starting control server", zap.String("addr", s.httpServer.Addr))
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("control server failed: %w", err)
	}
	return nil
}

// Shutdown gracefully shuts down the HTTP server
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down control server")
	return s.httpServer.Shutdown(ctx)
}

// handleCommand processes incoming control commands
func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	// Only accept POST requests
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Limit request body size to prevent abuse
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024) // 1MB max

	// Parse command from request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Warn("failed to read command request body", zap.Error(err))
		s.writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var cmd Command
	if err := json.Unmarshal(body, &cmd); err != nil {
		s.logger.Warn("failed to parse command JSON", zap.Error(err))
		s.writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	s.logger.Info("received control command",
		zap.String("command", string(cmd.Command)),
		zap.String("nonce", cmd.Nonce),
		zap.Time("timestamp", cmd.Timestamp),
	)

	// Handle the command
	result, err := s.handler.Handle(&cmd)
	if err != nil {
		s.logger.Error("command handling failed",
			zap.String("command", string(cmd.Command)),
			zap.Error(err),
		)
		// Return result even on error (includes verification failures)
		s.writeJSON(w, http.StatusOK, result)
		return
	}

	s.logger.Info("command executed successfully",
		zap.String("command", string(cmd.Command)),
		zap.String("message", result.Message),
	)

	s.writeJSON(w, http.StatusOK, result)
}

// handleHealth provides a simple health check endpoint
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"healthy"}`))
}

// writeJSON writes a JSON response
func (s *Server) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.logger.Error("failed to encode JSON response", zap.Error(err))
	}
}

// writeError writes an error response
func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]string{
		"error": message,
	})
}
