package security

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestRateLimiter_AllowWithinLimit(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(RateLimitConfig{MessagesPerMin: 5})

	for i := range 5 {
		if err := rl.Allow("sess1", "message"); err != nil {
			t.Fatalf("Allow(%d) returned error: %v", i, err)
		}
	}

	// 6th should be denied.
	if err := rl.Allow("sess1", "message"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
}

func TestRateLimiter_SlidingWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rl := NewRateLimiter(RateLimitConfig{MessagesPerMin: 2})
	rl.now = func() time.Time { return now }

	// Fill the bucket.
	_ = rl.Allow("sess1", "message")
	_ = rl.Allow("sess1", "message")

	// Should be denied.
	if err := rl.Allow("sess1", "message"); !errors.Is(err, ErrRateLimited) {
		t.Fatal("expected rate limit")
	}

	// Advance past the window.
	now = now.Add(61 * time.Second)

	// Should be allowed again.
	if err := rl.Allow("sess1", "message"); err != nil {
		t.Fatalf("expected allow after window, got %v", err)
	}
}

func TestRateLimiter_UnknownKind(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(RateLimitConfig{})

	// Unknown kind should always be allowed.
	if err := rl.Allow("sess1", "unknown_kind"); err != nil {
		t.Fatalf("expected nil for unknown kind, got %v", err)
	}
}

func TestRateLimiter_ToolCallBucket(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(RateLimitConfig{ToolCallsPerMin: 3})

	for range 3 {
		if err := rl.Allow("sess1", "tool_call"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if err := rl.Allow("sess1", "tool_call"); !errors.Is(err, ErrRateLimited) {
		t.Fatal("expected rate limit for tool_call")
	}
}

func TestRateLimiter_TokenBucket(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(RateLimitConfig{TokensPerHour: 100})

	if err := rl.AllowN("sess1", "token", 50); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := rl.AllowN("sess1", "token", 50); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := rl.AllowN("sess1", "token", 1); !errors.Is(err, ErrRateLimited) {
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
			_ = rl.Allow("sess1", "message")
		}()
	}
	wg.Wait()
}

func TestRateLimiter_AllowN_Unknown(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(RateLimitConfig{})

	if err := rl.AllowN("sess1", "nonexistent", 999); err != nil {
		t.Fatalf("expected nil for unknown kind, got %v", err)
	}
}

func TestRateLimiter_AllowN_ZeroAndNegative(t *testing.T) {
	t.Parallel()

	// Fill the bucket to the limit.
	rl := NewRateLimiter(RateLimitConfig{TokensPerHour: 1})
	if err := rl.AllowN("sess1", "token", 1); err != nil {
		t.Fatalf("unexpected error filling bucket: %v", err)
	}

	// Bucket is full, but AllowN(0) and AllowN(-1) should still return nil.
	if err := rl.AllowN("sess1", "token", 0); err != nil {
		t.Fatalf("AllowN(0) = %v, want nil", err)
	}
	if err := rl.AllowN("sess1", "token", -5); err != nil {
		t.Fatalf("AllowN(-5) = %v, want nil", err)
	}
}

func TestRateLimiter_PerSessionIsolation(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(RateLimitConfig{MessagesPerMin: 2})

	// Session A fills its bucket.
	_ = rl.Allow("sessA", "message")
	_ = rl.Allow("sessA", "message")

	// Session A is blocked.
	if err := rl.Allow("sessA", "message"); !errors.Is(err, ErrRateLimited) {
		t.Fatal("expected sessA to be rate limited")
	}

	// Session B should be unaffected.
	if err := rl.Allow("sessB", "message"); err != nil {
		t.Fatalf("sessB should not be rate limited, got %v", err)
	}
}

func TestRateLimiter_RemoveSession(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(RateLimitConfig{MessagesPerMin: 2, ToolCallsPerMin: 2})

	// Fill both buckets for sessA.
	_ = rl.Allow("sessA", "message")
	_ = rl.Allow("sessA", "message")
	_ = rl.Allow("sessA", "tool_call")

	// Verify sessA is tracked (3 bucket entries: 2 kinds).
	rl.mu.Lock()
	beforeCount := len(rl.buckets)
	rl.mu.Unlock()

	if beforeCount != 2 {
		t.Fatalf("expected 2 buckets for sessA, got %d", beforeCount)
	}

	// Remove session.
	rl.RemoveSession("sessA")

	rl.mu.Lock()
	afterCount := len(rl.buckets)
	rl.mu.Unlock()

	if afterCount != 0 {
		t.Errorf("expected 0 buckets after RemoveSession, got %d", afterCount)
	}

	// sessA should be able to use its limit again (fresh buckets).
	if err := rl.Allow("sessA", "message"); err != nil {
		t.Fatalf("expected allow after RemoveSession, got %v", err)
	}
}

func TestRateLimiter_RemoveSession_DoesNotAffectOthers(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(RateLimitConfig{MessagesPerMin: 2})

	// Fill both sessions.
	_ = rl.Allow("sessA", "message")
	_ = rl.Allow("sessB", "message")

	// Remove only sessA.
	rl.RemoveSession("sessA")

	// sessB buckets should still exist.
	rl.mu.Lock()
	count := len(rl.buckets)
	rl.mu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 bucket (sessB), got %d", count)
	}
}

func TestRateLimiter_PerSessionConcurrent(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(RateLimitConfig{MessagesPerMin: 50})

	var wg sync.WaitGroup
	for i := range 10 {
		sessID := fmt.Sprintf("sess%d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				_ = rl.Allow(sessID, "message")
			}
		}()
	}
	wg.Wait()

	// Each session should be at its limit — verify at least one is blocked.
	if err := rl.Allow("sess0", "message"); !errors.Is(err, ErrRateLimited) {
		t.Fatal("expected sess0 to be rate limited after 50 messages")
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
