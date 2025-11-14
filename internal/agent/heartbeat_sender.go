package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"linker-agent/internal/heartbeat"

	"go.uber.org/zap"
)

// QueueMetricsProvider defines an interface for getting queue metrics
type QueueMetricsProvider interface {
	Depth() int
	DLQSize() int
	DroppedCount() uint64
	EnqueuedCount() uint64
}

// HeartbeatSender sends periodic heartbeats to Connektn Cloud
type HeartbeatSender struct {
	agent              *Agent
	endpoint           string
	interval           time.Duration
	secret             string
	queueMetrics       QueueMetricsProvider
	logger             *zap.Logger
	httpClient         *http.Client
	lastFlushTimestamp time.Time
}

// HeartbeatSenderConfig holds heartbeat sender configuration
type HeartbeatSenderConfig struct {
	Endpoint     string
	Interval     time.Duration
	Secret       string
	QueueMetrics QueueMetricsProvider
	Logger       *zap.Logger
}

// NewHeartbeatSender creates a new heartbeat sender
func NewHeartbeatSender(agent *Agent, cfg HeartbeatSenderConfig) (*HeartbeatSender, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("heartbeat endpoint is required")
	}
	if cfg.Secret == "" {
		return nil, fmt.Errorf("heartbeat secret is required")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}

	return &HeartbeatSender{
		agent:        agent,
		endpoint:     cfg.Endpoint,
		interval:     cfg.Interval,
		secret:       cfg.Secret,
		queueMetrics: cfg.QueueMetrics,
		logger:       cfg.Logger,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		lastFlushTimestamp: time.Now().UTC(),
	}, nil
}

// Start begins sending heartbeats at the configured interval
func (s *HeartbeatSender) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.logger.Info("heartbeat sender started",
		zap.String("endpoint", s.endpoint),
		zap.Duration("interval", s.interval),
	)

	// Send initial heartbeat immediately
	if err := s.sendHeartbeat(ctx); err != nil {
		s.logger.Error("initial heartbeat failed", zap.Error(err))
	}

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("heartbeat sender stopped")
			return
		case <-ticker.C:
			if err := s.sendHeartbeat(ctx); err != nil {
				s.logger.Error("heartbeat failed", zap.Error(err))
			}
		}
	}
}

// sendHeartbeat sends a single heartbeat to the cloud
func (s *HeartbeatSender) sendHeartbeat(ctx context.Context) error {
	// Gather metrics
	var metrics heartbeat.QueueMetrics
	if s.queueMetrics != nil {
		metrics = heartbeat.QueueMetrics{
			Depth:        s.queueMetrics.Depth(),
			DLQSize:      s.queueMetrics.DLQSize(),
			DroppedCount: s.queueMetrics.DroppedCount(),
			RetryCount:   0, // TODO: Add retry count tracking
		}
	}

	// Create payload
	payload := heartbeat.NewPayload(
		s.agent.ID(),
		s.agent.OrganizationID(),
		s.agent.Version(),
		s.agent.startTime,
		s.agent.GetMode(),
		metrics,
		s.lastFlushTimestamp,
	)

	// Sign payload
	if err := payload.Sign(s.secret); err != nil {
		return fmt.Errorf("failed to sign heartbeat: %w", err)
	}

	// Marshal to JSON
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal heartbeat: %w", err)
	}

	// Send HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send heartbeat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("heartbeat request failed with status %d", resp.StatusCode)
	}

	s.logger.Debug("heartbeat sent successfully",
		zap.String("agent_id", s.agent.ID()),
		zap.Int("queue_depth", metrics.Depth),
		zap.Uint64("dropped_count", metrics.DroppedCount),
	)

	// Update last flush timestamp
	s.lastFlushTimestamp = time.Now().UTC()

	return nil
}
