package router

import (
	"sync"
	"testing"
	"time"
)

func TestPruning_LazyRateLimited(t *testing.T) {
	t.Parallel()

	store := NewInMemorySessionStore()
	laneLock := NewLaneLock()

	currentTime := time.Now()

	// Create a session with a timestamp in the past (1 hour ago).
	store.now = func() time.Time { return currentTime.Add(-time.Hour) }
	key := SessionKey{Channel: "slack", ChatID: "C1", ThreadID: "T1"}
	store.GetOrCreate(key)

	// Reset store.now to current time so Prune sees the session as idle.
	store.now = func() time.Time { return currentTime }

	pruner := newLazyPruner(store, laneLock, 10*time.Millisecond)
	pruner.now = func() time.Time { return currentTime }

	// First prune should run (enough time since zero lastRun).
	pruned := pruner.TryPrune()
	if pruned != 1 {
		t.Errorf("first TryPrune returned %d, want 1", pruned)
	}

	// Add another session with a past timestamp so it will be prunable.
	store.now = func() time.Time { return currentTime.Add(-time.Hour) }
	key2 := SessionKey{Channel: "slack", ChatID: "C2", ThreadID: "T2"}
	store.GetOrCreate(key2)
	store.now = func() time.Time { return currentTime }

	// Second prune immediately after should be rate-limited.
	pruned = pruner.TryPrune()
	if pruned != 0 {
		t.Errorf("second TryPrune returned %d, want 0 (rate-limited)", pruned)
	}

	// Advance time past the interval.
	currentTime = currentTime.Add(pruner.interval + time.Second)
	pruner.now = func() time.Time { return currentTime }
	store.now = func() time.Time { return currentTime }

	// Third prune should run and clean up the remaining session.
	pruned = pruner.TryPrune()
	if pruned != 1 {
		t.Errorf("third TryPrune returned %d, want 1", pruned)
	}
}

func TestPruning_NothingToPrune(t *testing.T) {
	t.Parallel()

	store := NewInMemorySessionStore()
	laneLock := NewLaneLock()
	pruner := newLazyPruner(store, laneLock, time.Hour)

	// No sessions → prune returns 0.
	pruned := pruner.TryPrune()
	if pruned != 0 {
		t.Errorf("TryPrune returned %d, want 0 for empty store", pruned)
	}
}

func TestPruning_CustomInterval(t *testing.T) {
	t.Parallel()

	store := NewInMemorySessionStore()
	laneLock := NewLaneLock()
	pruner := newLazyPruner(store, laneLock, time.Hour)
	pruner.interval = 100 * time.Millisecond

	currentTime := time.Now()
	pruner.now = func() time.Time { return currentTime }

	// First call runs.
	pruner.TryPrune()

	// Advance by less than interval.
	currentTime = currentTime.Add(50 * time.Millisecond)
	pruner.now = func() time.Time { return currentTime }

	pruned := pruner.TryPrune()
	if pruned != 0 {
		t.Errorf("TryPrune returned %d, want 0 within custom interval", pruned)
	}

	// Advance past interval.
	currentTime = currentTime.Add(100 * time.Millisecond)
	pruner.now = func() time.Time { return currentTime }

	// Should run again (0 pruned because no idle sessions, but it runs).
	pruned = pruner.TryPrune()
	if pruned != 0 {
		t.Errorf("TryPrune returned %d, want 0 (no idle sessions)", pruned)
	}
}

func TestPruning_CleansUpInactiveLanes(t *testing.T) {
	t.Parallel()

	store := NewInMemorySessionStore()
	laneLock := NewLaneLock()

	currentTime := time.Now()
	store.now = func() time.Time { return currentTime.Add(-time.Hour) }
	key := SessionKey{Channel: "slack", ChatID: "C1", ThreadID: "T1"}
	store.GetOrCreate(key)

	// Create lane entry for this session.
	laneLock.Acquire(key)
	laneLock.Release(key)

	store.now = func() time.Time { return currentTime }
	pruner := newLazyPruner(store, laneLock, 10*time.Millisecond)
	pruner.now = func() time.Time { return currentTime }

	pruned := pruner.TryPrune()
	if pruned != 1 {
		t.Fatalf("TryPrune returned %d, want 1", pruned)
	}

	laneLock.mu.Lock()
	_, exists := laneLock.lanes[key]
	laneLock.mu.Unlock()
	if exists {
		t.Error("lane should be cleaned up after session pruning")
	}
}

func TestPruning_ConcurrentTryPrune(t *testing.T) {
	t.Parallel()

	store := NewInMemorySessionStore()
	laneLock := NewLaneLock()
	pruner := newLazyPruner(store, laneLock, time.Hour)

	currentTime := time.Now()
	pruner.now = func() time.Time { return currentTime }
	pruner.interval = time.Hour

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pruner.TryPrune()
		}()
	}
	wg.Wait()
}
