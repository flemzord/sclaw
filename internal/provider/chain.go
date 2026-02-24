// Package provider defines the Provider interface for communicating with LLMs,
// health tracking with exponential backoff, and a failover chain orchestrator.
package provider

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// AuthProfile manages a set of API keys for a single provider,
// supporting rotation on rate limit errors.
type AuthProfile struct {
	mu   sync.Mutex
	keys []string
	idx  int
}

// NewAuthProfile creates an AuthProfile with the given keys.
// At least one key is required.
func NewAuthProfile(keys ...string) *AuthProfile {
	if len(keys) == 0 {
		panic("AuthProfile requires at least one key")
	}
	return &AuthProfile{keys: keys}
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

// Chain orchestrates failover across multiple providers.
// It is NOT itself a Provider — it adds role-based routing and
// health-aware failover on top.
type Chain struct {
	entries []chainEntry

	mu     sync.Mutex
	cancel context.CancelFunc
}

// NewChain creates a chain from the given entries.
func NewChain(entries []ChainEntry) (*Chain, error) {
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

	return &Chain{entries: internal}, nil
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
		}

		e.health.RecordFailure()
	}

	if lastErr != nil {
		return CompletionResponse{}, fmt.Errorf("%w: last error: %w", ErrAllProviders, lastErr)
	}
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
			e.health.RecordSuccess()
			return ch, nil
		}

		lastErr = err

		if !IsRetryable(err) {
			return nil, err
		}

		if IsRateLimit(err) && e.Auth != nil {
			e.Auth.Rotate()
		}

		e.health.RecordFailure()
	}

	if lastErr != nil {
		return nil, fmt.Errorf("%w: last error: %w", ErrAllProviders, lastErr)
	}
	return nil, fmt.Errorf("%w for role %q: all candidates unavailable", ErrAllProviders, role)
}

// minHealthCheckInterval returns the shortest configured check interval
// across all chain entries. Invalid or zero values fall back to defaults.
func minHealthCheckInterval(entries []chainEntry) time.Duration {
	if len(entries) == 0 {
		return 10 * time.Second
	}

	cfg := entries[0].Health
	cfg.defaults()
	interval := cfg.CheckInterval

	for i := 1; i < len(entries); i++ {
		cfg = entries[i].Health
		cfg.defaults()
		if cfg.CheckInterval < interval {
			interval = cfg.CheckInterval
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
