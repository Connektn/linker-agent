package stripewebhook

import (
	"sync"
)

// Metrics counters for webhook events.
// These are simple in-memory counters that can be exposed via /metrics endpoint.
// In production, these should be replaced with proper Prometheus metrics.

var (
	MetricsWebhookReceivedTotal   = &Counter{name: "webhook_received_total"}
	MetricsWebhookVerifiedTotal   = &Counter{name: "webhook_verified_total"}
	MetricsWebhookDupDroppedTotal = &Counter{name: "webhook_dup_dropped_total"}
	MetricsWebhookEnqueuedTotal   = &Counter{name: "webhook_enqueued_total"}
	MetricsWebhookErrorsTotal     = &LabeledCounter{
		name:     "webhook_errors_total",
		counters: make(map[string]*Counter),
	}
	MetricsWebhookProcessedTotal = &LabeledCounter{
		name:     "webhook_processed_total",
		counters: make(map[string]*Counter),
	}
)

// Counter is a simple thread-safe counter.
type Counter struct {
	name  string
	mu    sync.Mutex
	value int64
}

// Inc increments the counter by 1.
func (c *Counter) Inc() {
	c.mu.Lock()
	c.value++
	c.mu.Unlock()
}

// Value returns the current counter value.
func (c *Counter) Value() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

// Name returns the counter name.
func (c *Counter) Name() string {
	return c.name
}

// LabeledCounter is a thread-safe counter with label support.
type LabeledCounter struct {
	name     string
	mu       sync.RWMutex
	counters map[string]*Counter
}

// WithLabelValues returns a counter for the given label values.
func (lc *LabeledCounter) WithLabelValues(labels ...string) *Counter {
	// Create a simple key from labels (for MVP, just join them)
	key := ""
	if len(labels) > 0 {
		key = labels[0]
	}

	lc.mu.RLock()
	c, exists := lc.counters[key]
	lc.mu.RUnlock()

	if exists {
		return c
	}

	lc.mu.Lock()
	defer lc.mu.Unlock()

	// Double-check after acquiring write lock
	c, exists = lc.counters[key]
	if exists {
		return c
	}

	c = &Counter{name: lc.name + "{" + key + "}"}
	lc.counters[key] = c
	return c
}

// All returns all labeled counters.
func (lc *LabeledCounter) All() map[string]*Counter {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	// Return a copy to prevent concurrent modification
	result := make(map[string]*Counter, len(lc.counters))
	for k, v := range lc.counters {
		result[k] = v
	}
	return result
}

// Name returns the counter name.
func (lc *LabeledCounter) Name() string {
	return lc.name
}
