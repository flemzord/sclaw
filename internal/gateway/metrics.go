package gateway

import (
	"sync/atomic"
	"time"
)

// Metrics tracks gateway-level counters using atomic operations for lock-free concurrency.
type Metrics struct {
	completions  atomic.Int64
	messages     atomic.Int64
	errors       atomic.Int64
	totalTokens  atomic.Int64
	totalLatency atomic.Int64 // nanoseconds
}

// RecordCompletion records a successful LLM completion.
func (m *Metrics) RecordCompletion(tokens int, latency time.Duration) {
	m.completions.Add(1)
	m.totalTokens.Add(int64(tokens))
	m.totalLatency.Add(int64(latency))
}

// RecordMessage records an inbound message.
func (m *Metrics) RecordMessage() {
	m.messages.Add(1)
}

// RecordError records a processing error.
func (m *Metrics) RecordError() {
	m.errors.Add(1)
}

// Snapshot returns a consistent point-in-time view of the counters.
func (m *Metrics) Snapshot() MetricsSnapshot {
	completions := m.completions.Load()
	snap := MetricsSnapshot{
		Completions: completions,
		Messages:    m.messages.Load(),
		Errors:      m.errors.Load(),
		TotalTokens: m.totalTokens.Load(),
	}
	if completions > 0 {
		snap.AvgLatency = time.Duration(m.totalLatency.Load() / completions)
	}
	return snap
}

// MetricsSnapshot is a serializable point-in-time metrics view.
type MetricsSnapshot struct {
	Completions int64         `json:"completions"`
	Messages    int64         `json:"messages"`
	Errors      int64         `json:"errors"`
	TotalTokens int64         `json:"total_tokens"`
	AvgLatency  time.Duration `json:"avg_latency_ns"`
}
