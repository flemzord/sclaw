package security

import (
	"errors"
	"sync"
	"time"
)

// ErrRateLimited is returned when a request exceeds the rate limit.
var ErrRateLimited = errors.New("rate limit exceeded")

// RateLimitConfig holds configurable rate limits.
type RateLimitConfig struct {
	MaxSessions     int `yaml:"max_sessions"`
	MessagesPerMin  int `yaml:"messages_per_min"`
	ToolCallsPerMin int `yaml:"tool_calls_per_min"`
	TokensPerHour   int `yaml:"tokens_per_hour"`
}

// rateLimitConfigDefaults returns a config with sensible defaults.
func rateLimitConfigDefaults() RateLimitConfig {
	return RateLimitConfig{
		MaxSessions:     100,
		MessagesPerMin:  200,
		ToolCallsPerMin: 500,
		TokensPerHour:   0, // 0 = unlimited
	}
}

// RateLimiter implements sliding window rate limiting using stdlib only.
// Each bucket tracks timestamps of recent events within its window.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	config  RateLimitConfig
	now     func() time.Time
}

type bucket struct {
	window time.Duration
	limit  int
	events []time.Time
}

// NewRateLimiter creates a rate limiter with the given config.
// Zero-value fields in cfg are replaced with defaults.
func NewRateLimiter(cfg RateLimitConfig) *RateLimiter {
	defaults := rateLimitConfigDefaults()
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = defaults.MaxSessions
	}
	if cfg.MessagesPerMin <= 0 {
		cfg.MessagesPerMin = defaults.MessagesPerMin
	}
	if cfg.ToolCallsPerMin <= 0 {
		cfg.ToolCallsPerMin = defaults.ToolCallsPerMin
	}

	rl := &RateLimiter{
		config: cfg,
		now:    time.Now,
		buckets: map[string]*bucket{
			"message": {
				window: time.Minute,
				limit:  cfg.MessagesPerMin,
			},
			"tool_call": {
				window: time.Minute,
				limit:  cfg.ToolCallsPerMin,
			},
		},
	}

	if cfg.TokensPerHour > 0 {
		rl.buckets["token"] = &bucket{
			window: time.Hour,
			limit:  cfg.TokensPerHour,
		}
	}

	return rl
}

// Allow checks whether an event of the given kind is allowed.
// Returns nil if allowed, ErrRateLimited if the limit is exceeded.
// kind must be one of: "message", "tool_call", "token".
func (rl *RateLimiter) Allow(kind string) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[kind]
	if !ok {
		// Unknown kind = no limit configured.
		return nil
	}

	now := rl.now()
	b.evict(now)

	if len(b.events) >= b.limit {
		return ErrRateLimited
	}

	b.events = append(b.events, now)
	return nil
}

// AllowN checks whether n events of the given kind are allowed.
// Useful for token counting where a single request consumes multiple tokens.
func (rl *RateLimiter) AllowN(kind string, n int) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[kind]
	if !ok {
		return nil
	}

	now := rl.now()
	b.evict(now)

	if len(b.events)+n > b.limit {
		return ErrRateLimited
	}

	for range n {
		b.events = append(b.events, now)
	}
	return nil
}

// MaxSessions returns the configured maximum number of concurrent sessions.
func (rl *RateLimiter) MaxSessions() int {
	return rl.config.MaxSessions
}

// evict removes events outside the sliding window.
func (b *bucket) evict(now time.Time) {
	cutoff := now.Add(-b.window)
	// Find the first event within the window (events are chronologically ordered).
	i := 0
	for i < len(b.events) && b.events[i].Before(cutoff) {
		i++
	}
	if i > 0 {
		b.events = b.events[i:]
	}
}
