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

// bucketTemplate defines the window and limit for a rate limit kind.
type bucketTemplate struct {
	window time.Duration
	limit  int
}

// RateLimiter implements per-session sliding window rate limiting using stdlib only.
// Each session gets independent buckets keyed by "sessionID:kind".
type RateLimiter struct {
	mu        sync.Mutex
	buckets   map[string]*bucket
	templates map[string]bucketTemplate
	config    RateLimitConfig
	now       func() time.Time
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

	templates := map[string]bucketTemplate{
		"message": {
			window: time.Minute,
			limit:  cfg.MessagesPerMin,
		},
		"tool_call": {
			window: time.Minute,
			limit:  cfg.ToolCallsPerMin,
		},
	}

	if cfg.TokensPerHour > 0 {
		templates["token"] = bucketTemplate{
			window: time.Hour,
			limit:  cfg.TokensPerHour,
		}
	}

	return &RateLimiter{
		config:    cfg,
		now:       time.Now,
		buckets:   make(map[string]*bucket),
		templates: templates,
	}
}

// bucketKey returns the per-session bucket key.
func bucketKey(sessionID, kind string) string {
	return sessionID + ":" + kind
}

// getOrCreateBucket returns the bucket for the given session and kind,
// creating it lazily from the template if it doesn't exist yet.
func (rl *RateLimiter) getOrCreateBucket(sessionID, kind string) *bucket {
	tmpl, ok := rl.templates[kind]
	if !ok {
		return nil
	}

	key := bucketKey(sessionID, kind)
	b, ok := rl.buckets[key]
	if !ok {
		b = &bucket{
			window: tmpl.window,
			limit:  tmpl.limit,
		}
		rl.buckets[key] = b
	}
	return b
}

// Allow checks whether an event of the given kind is allowed for the session.
// Returns nil if allowed, ErrRateLimited if the limit is exceeded.
// kind must be one of: "message", "tool_call", "token".
func (rl *RateLimiter) Allow(sessionID, kind string) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b := rl.getOrCreateBucket(sessionID, kind)
	if b == nil {
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

// TODO: For high-volume token counting, consider storing a counter per time
// bucket rather than individual timestamps to reduce memory usage.

// AllowN checks whether n events of the given kind are allowed for the session.
// Useful for token counting where a single request consumes multiple tokens.
func (rl *RateLimiter) AllowN(sessionID, kind string, n int) error {
	if n <= 0 {
		return nil
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b := rl.getOrCreateBucket(sessionID, kind)
	if b == nil {
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

// RemoveSession evicts all rate limit buckets for the given session.
func (rl *RateLimiter) RemoveSession(sessionID string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	prefix := sessionID + ":"
	for key := range rl.buckets {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(rl.buckets, key)
		}
	}
}

// MaxSessions returns the configured maximum number of concurrent sessions.
func (rl *RateLimiter) MaxSessions() int {
	return rl.config.MaxSessions
}

// evict removes events outside the sliding window.
// It uses the copy-and-clear pattern to release backing array memory
// and avoid unbounded growth of the underlying array.
func (b *bucket) evict(now time.Time) {
	cutoff := now.Add(-b.window)
	i := 0
	for i < len(b.events) && b.events[i].Before(cutoff) {
		i++
	}
	if i > 0 {
		remaining := len(b.events) - i
		copy(b.events, b.events[i:])
		clear(b.events[remaining:])
		b.events = b.events[:remaining]
	}
}
