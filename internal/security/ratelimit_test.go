package security

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRateLimiter_AllowWithinLimit(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(RateLimitConfig{MessagesPerMin: 5})

	for i := range 5 {
		if err := rl.Allow("message"); err != nil {
			t.Fatalf("Allow(%d) returned error: %v", i, err)
		}
	}

	// 6th should be denied.
	if err := rl.Allow("message"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
}

func TestRateLimiter_SlidingWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rl := NewRateLimiter(RateLimitConfig{MessagesPerMin: 2})
	rl.now = func() time.Time { return now }

	// Fill the bucket.
	_ = rl.Allow("message")
	_ = rl.Allow("message")

	// Should be denied.
	if err := rl.Allow("message"); !errors.Is(err, ErrRateLimited) {
		t.Fatal("expected rate limit")
	}

	// Advance past the window.
	now = now.Add(61 * time.Second)

	// Should be allowed again.
	if err := rl.Allow("message"); err != nil {
		t.Fatalf("expected allow after window, got %v", err)
	}
}

func TestRateLimiter_UnknownKind(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(RateLimitConfig{})

	// Unknown kind should always be allowed.
	if err := rl.Allow("unknown_kind"); err != nil {
		t.Fatalf("expected nil for unknown kind, got %v", err)
	}
}

func TestRateLimiter_ToolCallBucket(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(RateLimitConfig{ToolCallsPerMin: 3})

	for range 3 {
		if err := rl.Allow("tool_call"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if err := rl.Allow("tool_call"); !errors.Is(err, ErrRateLimited) {
		t.Fatal("expected rate limit for tool_call")
	}
}

func TestRateLimiter_TokenBucket(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(RateLimitConfig{TokensPerHour: 100})

	if err := rl.AllowN("token", 50); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := rl.AllowN("token", 50); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := rl.AllowN("token", 1); !errors.Is(err, ErrRateLimited) {
		t.Fatal("expected rate limit for tokens")
	}
}

func TestRateLimiter_MaxSessions(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(RateLimitConfig{MaxSessions: 42})
	if rl.MaxSessions() != 42 {
		t.Fatalf("MaxSessions() = %d, want 42", rl.MaxSessions())
	}
}

func TestRateLimiter_Defaults(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(RateLimitConfig{})

	if rl.config.MaxSessions != 100 {
		t.Errorf("default MaxSessions = %d, want 100", rl.config.MaxSessions)
	}
	if rl.config.MessagesPerMin != 200 {
		t.Errorf("default MessagesPerMin = %d, want 200", rl.config.MessagesPerMin)
	}
	if rl.config.ToolCallsPerMin != 500 {
		t.Errorf("default ToolCallsPerMin = %d, want 500", rl.config.ToolCallsPerMin)
	}
}

func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(RateLimitConfig{MessagesPerMin: 1000})

	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = rl.Allow("message")
		}()
	}
	wg.Wait()
}

func TestRateLimiter_AllowN_Unknown(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(RateLimitConfig{})

	if err := rl.AllowN("nonexistent", 999); err != nil {
		t.Fatalf("expected nil for unknown kind, got %v", err)
	}
}

func TestRateLimiter_AllowN_ZeroAndNegative(t *testing.T) {
	t.Parallel()

	// Fill the bucket to the limit.
	rl := NewRateLimiter(RateLimitConfig{TokensPerHour: 1})
	if err := rl.AllowN("token", 1); err != nil {
		t.Fatalf("unexpected error filling bucket: %v", err)
	}

	// Bucket is full, but AllowN(0) and AllowN(-1) should still return nil.
	if err := rl.AllowN("token", 0); err != nil {
		t.Fatalf("AllowN(0) = %v, want nil", err)
	}
	if err := rl.AllowN("token", -5); err != nil {
		t.Fatalf("AllowN(-5) = %v, want nil", err)
	}
}

func TestBucketEvict_ReleasesMemory(t *testing.T) {
	t.Parallel()

	// Use a 1-second window and add many events spread over multiple windows.
	// After eviction, the backing array should not grow unbounded.
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b := &bucket{
		window: time.Second,
		limit:  1000,
	}

	const rounds = 50
	const eventsPerRound = 10

	for round := range rounds {
		// Advance time by 2 seconds to expire all previous events.
		now = now.Add(2 * time.Second)
		for range eventsPerRound {
			b.evict(now)
			b.events = append(b.events, now)
		}
		_ = round
	}

	// After many eviction cycles, the slice cap should be bounded.
	// With the copy-clear pattern, cap stays close to the live window size,
	// not growing proportional to total events ever added.
	maxExpectedCap := eventsPerRound * 4 // generous headroom
	if cap(b.events) > maxExpectedCap {
		t.Errorf("cap(b.events) = %d after %d rounds, want <= %d (memory leak detected)",
			cap(b.events), rounds, maxExpectedCap)
	}
}
