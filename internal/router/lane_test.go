package router

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLaneLock_SameSession_Serial(t *testing.T) {
	t.Parallel()

	ll := NewLaneLock()
	key := SessionKey{Channel: "slack", ChatID: "C1", ThreadID: "T1"}

	// counter tracks the number of goroutines currently in the critical section.
	// If serialization works, it should never exceed 1.
	var counter atomic.Int32
	var maxConcurrent atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ll.Acquire(key)
			defer ll.Release(key)

			cur := counter.Add(1)
			// Track the maximum concurrent occupancy.
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}

			// Simulate work to give other goroutines a chance to race.
			time.Sleep(time.Millisecond)

			counter.Add(-1)
		}()
	}

	wg.Wait()

	if peak := maxConcurrent.Load(); peak != 1 {
		t.Errorf("max concurrent goroutines in critical section = %d, want 1", peak)
	}
}

func TestLaneLock_DifferentSession_Parallel(t *testing.T) {
	t.Parallel()

	ll := NewLaneLock()
	keyA := SessionKey{Channel: "slack", ChatID: "A"}
	keyB := SessionKey{Channel: "slack", ChatID: "B"}

	// Both goroutines signal when they enter the critical section.
	enteredA := make(chan struct{})
	enteredB := make(chan struct{})
	done := make(chan struct{})

	go func() {
		ll.Acquire(keyA)
		close(enteredA)
		// Wait for B to also enter before releasing.
		<-enteredB
		ll.Release(keyA)
	}()

	go func() {
		ll.Acquire(keyB)
		close(enteredB)
		// Wait for A to also enter before releasing.
		<-enteredA
		ll.Release(keyB)
		close(done)
	}()

	// If the two goroutines can be in their critical sections simultaneously,
	// this will complete quickly. If they were serialized, it would deadlock
	// (each waits for the other to enter).
	select {
	case <-done:
		// Success: both goroutines ran in parallel.
	case <-time.After(2 * time.Second):
		t.Fatal("timed out: different sessions should run in parallel")
	}
}

func TestLaneLock_Cleanup(t *testing.T) {
	t.Parallel()

	ll := NewLaneLock()
	keyA := SessionKey{Channel: "slack", ChatID: "A"}
	keyB := SessionKey{Channel: "slack", ChatID: "B"}
	keyC := SessionKey{Channel: "slack", ChatID: "C"}

	// Acquire and release all three to populate the lane map.
	for _, key := range []SessionKey{keyA, keyB, keyC} {
		ll.Acquire(key)
		ll.Release(key)
	}

	// Only keyA is still active.
	activeKeys := map[SessionKey]struct{}{
		keyA: {},
	}
	ll.Cleanup(activeKeys)

	// Verify: keyA lane still exists, keyB and keyC are removed.
	ll.mu.Lock()
	defer ll.mu.Unlock()

	if _, ok := ll.lanes[keyA]; !ok {
		t.Error("keyA lane should still exist after cleanup")
	}
	if _, ok := ll.lanes[keyB]; ok {
		t.Error("keyB lane should have been removed by cleanup")
	}
	if _, ok := ll.lanes[keyC]; ok {
		t.Error("keyC lane should have been removed by cleanup")
	}
}

func TestLaneLock_AcquireRelease_NoDeadlock(t *testing.T) {
	t.Parallel()

	ll := NewLaneLock()
	key := SessionKey{Channel: "test", ChatID: "1"}

	// Rapid acquire/release cycles should not deadlock.
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ll.Acquire(key)
			ll.Release(key)
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success.
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock detected: rapid acquire/release cycles did not complete")
	}
}
