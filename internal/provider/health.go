package provider

import (
	"sync"
	"time"
)

// healthState represents the current availability state of a provider.
type healthState int

const (
	stateHealthy  healthState = iota
	stateCooldown             // transient failure, backing off
	stateDead                 // too many consecutive failures
)

// HealthConfig controls health tracking behavior.
type HealthConfig struct {
	// InitialBackoff is the cooldown duration after the first failure.
	// Default: 1s.
	InitialBackoff time.Duration

	// MaxBackoff caps the exponential backoff duration.
	// Default: 60s.
	MaxBackoff time.Duration

	// MaxFailures is the number of consecutive failures before the
	// provider is marked dead. Default: 5.
	MaxFailures int

	// CheckInterval is how often the background goroutine probes
	// dead/cooldown providers. Default: 10s.
	CheckInterval time.Duration
}

// defaults fills zero-value fields with sensible defaults.
func (c *HealthConfig) defaults() {
	if c.InitialBackoff <= 0 {
		c.InitialBackoff = time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 60 * time.Second
	}
	if c.MaxFailures <= 0 {
		c.MaxFailures = 5
	}
	if c.CheckInterval <= 0 {
		c.CheckInterval = 10 * time.Second
	}
}

// healthTracker monitors the availability of a single provider.
// It implements exponential backoff on failures and marks the
// provider dead after MaxFailures consecutive failures.
type healthTracker struct {
	cfg HealthConfig

	mu              sync.Mutex
	state           healthState
	failures        int
	currentBackoff  time.Duration
	cooldownExpires time.Time

	// now is injectable for testing. Defaults to time.Now.
	now func() time.Time
}

// newHealthTracker creates a healthy tracker with the given config.
func newHealthTracker(cfg HealthConfig) *healthTracker {
	cfg.defaults()
	return &healthTracker{
		cfg:   cfg,
		state: stateHealthy,
		now:   time.Now,
	}
}

// IsAvailable reports whether the provider can accept requests.
// A provider in cooldown becomes available once its backoff expires.
func (h *healthTracker) IsAvailable() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch h.state {
	case stateHealthy:
		return true
	case stateCooldown:
		return !h.now().Before(h.cooldownExpires)
	default: // stateDead
		return false
	}
}

// RecordSuccess resets the tracker to the healthy state.
func (h *healthTracker) RecordSuccess() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.state = stateHealthy
	h.failures = 0
	h.currentBackoff = 0
}

// RecordFailure records a failed request. It transitions the tracker
// to cooldown (with exponential backoff) or dead after MaxFailures.
func (h *healthTracker) RecordFailure() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.failures++

	if h.failures >= h.cfg.MaxFailures {
		h.state = stateDead
		return
	}

	h.state = stateCooldown

	if h.currentBackoff == 0 {
		h.currentBackoff = h.cfg.InitialBackoff
	} else {
		h.currentBackoff *= 2
	}
	if h.currentBackoff > h.cfg.MaxBackoff {
		h.currentBackoff = h.cfg.MaxBackoff
	}

	h.cooldownExpires = h.now().Add(h.currentBackoff)
}

// ShouldHealthCheck reports whether the provider needs an active
// health probe. This is true for dead and cooldown-expired providers.
func (h *healthTracker) ShouldHealthCheck() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch h.state {
	case stateDead:
		return true
	case stateCooldown:
		return !h.now().Before(h.cooldownExpires)
	default:
		return false
	}
}

// State returns the current health state. Exported for testing.
func (h *healthTracker) State() healthState {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state
}

// Failures returns the current consecutive failure count.
func (h *healthTracker) Failures() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.failures
}
