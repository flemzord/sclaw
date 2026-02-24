// Package provider defines the Provider interface for communicating with LLMs,
// health tracking with exponential backoff, and a failover chain orchestrator.
package provider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// nopHandler is a slog.Handler that discards all log records.
// Enabled returns false so slog skips formatting entirely (zero cost).
type nopHandler struct{}

func (nopHandler) Enabled(context.Context, slog.Level) bool {
	return false
}

func (nopHandler) Handle(context.Context, slog.Record) error {
	return nil
}

func (nopHandler) WithAttrs([]slog.Attr) slog.Handler {
	return nopHandler{}
}

func (nopHandler) WithGroup(string) slog.Handler {
	return nopHandler{}
}

// AuthProfile manages a set of API keys for a single provider,
// supporting rotation on rate limit errors.
type AuthProfile struct {
	mu   sync.Mutex
	keys []string
	idx  int
}

// ErrNoKeys is returned when NewAuthProfile is called without any keys.
var ErrNoKeys = errors.New("AuthProfile requires at least one key")

// NewAuthProfile creates an AuthProfile with the given keys.
// At least one key is required.
func NewAuthProfile(keys ...string) (*AuthProfile, error) {
	if len(keys) == 0 {
		return nil, ErrNoKeys
	}
	return &AuthProfile{keys: keys}, nil
}

// CurrentKey returns the currently active API key.
func (a *AuthProfile) CurrentKey() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.keys[a.idx]
}

// Rotate advances to the next key in the list, wrapping around.
// Returns true if rotation happened (i.e. more than one key exists).
func (a *AuthProfile) Rotate() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.keys) <= 1 {
		return false
	}
	a.idx = (a.idx + 1) % len(a.keys)
	return true
}

// CurrentIndex returns the zero-based index of the active key.
func (a *AuthProfile) CurrentIndex() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.idx
}

// ChainEntry configures a single provider in the chain.
type ChainEntry struct {
	Name        string
	Provider    Provider
	Role        Role
	Auth        *AuthProfile
	Health      HealthConfig
	FallbackFor []Role // empty = fallback for all roles
}

// chainEntry is the internal representation with health tracking.
type chainEntry struct {
	ChainEntry
	health *healthTracker
}

// ChainOption configures optional Chain behavior.
type ChainOption func(*Chain)

// WithLogger injects a structured logger into the Chain.
// When nil or omitted, all log output is silently discarded (zero cost).
func WithLogger(l *slog.Logger) ChainOption {
	return func(c *Chain) { c.logger = l }
}

// Chain orchestrates failover across multiple providers.
// It is NOT itself a Provider — it adds role-based routing and
// health-aware failover on top.
type Chain struct {
	entries []chainEntry
	logger  *slog.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
}

// NewChain creates a chain from the given entries.
func NewChain(entries []ChainEntry, opts ...ChainOption) (*Chain, error) {
	if len(entries) == 0 {
		return nil, ErrNoProvider
	}

	internal := make([]chainEntry, len(entries))
	for i, e := range entries {
		if e.Provider == nil {
			return nil, fmt.Errorf("%w: entry %q has nil provider", ErrNoProvider, e.Name)
		}
		internal[i] = chainEntry{
			ChainEntry: e,
			health:     newHealthTracker(e.Health),
		}
	}

	c := &Chain{entries: internal}

	for _, opt := range opts {
		opt(c)
	}

	if c.logger == nil {
		c.logger = slog.New(nopHandler{})
	}

	// Wire up health state change callbacks for observability.
	for i := range c.entries {
		e := &c.entries[i]
		name := e.Name
		logger := c.logger
		e.health.onStateChange = func(from, to healthState) {
			switch to {
			case stateCooldown:
				logger.Warn("provider entered cooldown",
					"provider", name,
					"backoff", e.health.CurrentBackoff(),
					"failures", e.health.Failures(),
				)
			case stateDead:
				logger.Error("provider marked dead",
					"provider", name,
					"total_failures", e.health.Failures(),
				)
			case stateHealthy:
				logger.Info("provider revived",
					"provider", name,
					"previous_state", from.String(),
				)
			}
		}
	}

	return c, nil
}

// Start launches background health check goroutines.
func (pc *Chain) Start(ctx context.Context) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if pc.cancel != nil {
		return // already started
	}

	ctx, pc.cancel = context.WithCancel(ctx)

	interval := minHealthCheckInterval(pc.entries)

	go runHealthChecks(ctx, interval, pc.entries)
}

// Stop cancels background health checks.
func (pc *Chain) Stop() {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if pc.cancel != nil {
		pc.cancel()
		pc.cancel = nil
	}
}

// Complete sends a completion request to the best available provider
// for the given role, with automatic failover.
func (pc *Chain) Complete(ctx context.Context, role Role, req CompletionRequest) (CompletionResponse, error) {
	candidates := pc.candidates(role)
	if len(candidates) == 0 {
		return CompletionResponse{}, fmt.Errorf("%w for role %q", ErrNoProvider, role)
	}

	var lastErr error
	for _, e := range candidates {
		if err := ctx.Err(); err != nil {
			return CompletionResponse{}, err
		}
		if !e.health.IsAvailable() {
			continue
		}

		resp, err := e.Provider.Complete(ctx, req)
		if err == nil {
			e.health.RecordSuccess()
			return resp, nil
		}

		lastErr = err

		// Non-retryable errors stop failover.
		if !IsRetryable(err) {
			return CompletionResponse{}, err
		}

		// Rotate auth on rate limit before recording failure.
		if IsRateLimit(err) && e.Auth != nil {
			e.Auth.Rotate()
			pc.logger.Info("auth key rotated",
				"provider", e.Name,
				"key_index", e.Auth.CurrentIndex(),
			)
		}

		e.health.RecordFailure()

		pc.logger.Warn("provider failed, failing over",
			"provider", e.Name,
			"error", err,
		)
	}

	if lastErr != nil {
		pc.logger.Error("all providers exhausted",
			"role", role,
			"last_error", lastErr,
		)
		return CompletionResponse{}, fmt.Errorf("%w: last error: %w", ErrAllProviders, lastErr)
	}
	pc.logger.Error("all providers exhausted",
		"role", role,
	)
	return CompletionResponse{}, fmt.Errorf("%w for role %q: all candidates unavailable", ErrAllProviders, role)
}

// Stream sends a streaming completion request to the best available
// provider for the given role, with automatic failover.
func (pc *Chain) Stream(ctx context.Context, role Role, req CompletionRequest) (<-chan StreamChunk, error) {
	candidates := pc.candidates(role)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w for role %q", ErrNoProvider, role)
	}

	var lastErr error
	for _, e := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !e.health.IsAvailable() {
			continue
		}

		ch, err := e.Provider.Stream(ctx, req)
		if err == nil {
			return pc.wrapStream(ch, e), nil
		}

		lastErr = err

		if !IsRetryable(err) {
			return nil, err
		}

		if IsRateLimit(err) && e.Auth != nil {
			e.Auth.Rotate()
			pc.logger.Info("auth key rotated",
				"provider", e.Name,
				"key_index", e.Auth.CurrentIndex(),
			)
		}

		e.health.RecordFailure()

		pc.logger.Warn("provider failed, failing over",
			"provider", e.Name,
			"error", err,
		)
	}

	if lastErr != nil {
		pc.logger.Error("all providers exhausted",
			"role", role,
			"last_error", lastErr,
		)
		return nil, fmt.Errorf("%w: last error: %w", ErrAllProviders, lastErr)
	}
	pc.logger.Error("all providers exhausted",
		"role", role,
	)
	return nil, fmt.Errorf("%w for role %q: all candidates unavailable", ErrAllProviders, role)
}

// wrapStream wraps a provider's stream channel to defer the health verdict.
// RecordSuccess is only called if the entire stream completes without retryable errors.
// Mid-stream retryable errors trigger RecordFailure immediately.
func (pc *Chain) wrapStream(src <-chan StreamChunk, e *chainEntry) <-chan StreamChunk {
	out := make(chan StreamChunk, cap(src))
	go func() {
		defer close(out)
		var sawError bool
		for chunk := range src {
			if chunk.Err != nil && IsRetryable(chunk.Err) {
				sawError = true
				e.health.RecordFailure()
				pc.logger.Warn("mid-stream error degraded provider health",
					"provider", e.Name,
					"error", chunk.Err,
				)
			}
			out <- chunk
		}
		if !sawError {
			e.health.RecordSuccess()
		}
	}()
	return out
}

// minHealthCheckInterval returns the shortest configured check interval
// across all chain entries. Invalid or zero values fall back to defaults.
func minHealthCheckInterval(entries []chainEntry) time.Duration {
	if len(entries) == 0 {
		return 10 * time.Second
	}

	interval := entries[0].Health.checkIntervalOrDefault()
	for i := 1; i < len(entries); i++ {
		if d := entries[i].Health.checkIntervalOrDefault(); d < interval {
			interval = d
		}
	}
	return interval
}

// GetProvider returns the first available provider for the given role.
func (pc *Chain) GetProvider(role Role) (Provider, error) {
	candidates := pc.candidates(role)
	for _, e := range candidates {
		if e.health.IsAvailable() {
			return e.Provider, nil
		}
	}
	return nil, fmt.Errorf("%w for role %q", ErrNoProvider, role)
}

// candidates returns chain entries matching the given role.
// Direct role matches come first, then fallback entries.
func (pc *Chain) candidates(role Role) []*chainEntry {
	var direct, fallbacks []*chainEntry

	for i := range pc.entries {
		e := &pc.entries[i]
		if e.Role == role {
			direct = append(direct, e)
			continue
		}
		if e.Role == RoleFallback && pc.isFallbackFor(e, role) {
			fallbacks = append(fallbacks, e)
		}
	}

	return append(direct, fallbacks...)
}

// isFallbackFor checks if a fallback entry covers the given role.
func (pc *Chain) isFallbackFor(e *chainEntry, role Role) bool {
	if len(e.FallbackFor) == 0 {
		return true // empty = all roles
	}
	for _, r := range e.FallbackFor {
		if r == role {
			return true
		}
	}
	return false
}

// IsRateLimit reports whether err is or wraps ErrRateLimit.
func IsRateLimit(err error) bool {
	return errors.Is(err, ErrRateLimit)
}

// runHealthChecks runs periodic health probes for all entries.
// It blocks until the context is cancelled.
func runHealthChecks(ctx context.Context, interval time.Duration, entries []chainEntry) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for i := range entries {
				e := &entries[i]
				if !e.health.ShouldHealthCheck() {
					continue
				}

				checker, ok := e.Provider.(HealthChecker)
				if !ok {
					continue
				}

				if err := checker.HealthCheck(ctx); err == nil {
					e.health.RecordSuccess()
				}
			}
		}
	}
}
