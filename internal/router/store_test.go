package router

import (
	"sync"
	"testing"
	"time"
)

// fakeTime provides an injectable clock for deterministic testing.
// Same pattern used in internal/provider/health_test.go.
type fakeTime struct {
	mu      sync.Mutex
	current time.Time
}

func (f *fakeTime) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.current
}

func (f *fakeTime) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current = f.current.Add(d)
}

func newTestStore() (*InMemorySessionStore, *fakeTime) {
	s := NewInMemorySessionStore()
	ft := &fakeTime{current: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}
	s.now = ft.Now
	return s, ft
}

func TestInMemoryStore_GetOrCreate(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore()
	key := SessionKey{Channel: "slack", ChatID: "C1", ThreadID: "T1"}

	// First call creates a new session.
	sess1, created := store.GetOrCreate(key)
	if !created {
		t.Fatal("expected created=true on first call")
	}
	if sess1 == nil {
		t.Fatal("session should not be nil")
	}
	if sess1.ID == "" {
		t.Error("session ID should not be empty")
	}
	if len(sess1.ID) != 32 {
		t.Errorf("session ID length = %d, want 32 hex chars", len(sess1.ID))
	}
	if sess1.Key != key {
		t.Errorf("session Key = %v, want %v", sess1.Key, key)
	}

	// Second call returns the same session.
	sess2, created := store.GetOrCreate(key)
	if created {
		t.Fatal("expected created=false on second call")
	}
	if sess2.ID != sess1.ID {
		t.Errorf("second call returned different ID: %q vs %q", sess2.ID, sess1.ID)
	}

	if store.Len() != 1 {
		t.Errorf("Len() = %d, want 1", store.Len())
	}
}

func TestInMemoryStore_Get(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore()
	key := SessionKey{Channel: "slack", ChatID: "C1"}

	// Get on missing key returns nil.
	if got := store.Get(key); got != nil {
		t.Fatalf("Get on missing key returned %v, want nil", got)
	}

	store.GetOrCreate(key)

	// Get on existing key returns the session.
	got := store.Get(key)
	if got == nil {
		t.Fatal("Get returned nil for existing session")
	}
	if got.Key != key {
		t.Errorf("Key = %v, want %v", got.Key, key)
	}
}

func TestInMemoryStore_Touch(t *testing.T) {
	t.Parallel()

	store, ft := newTestStore()
	key := SessionKey{Channel: "slack", ChatID: "C1"}

	sess, _ := store.GetOrCreate(key)
	original := sess.LastActiveAt

	ft.Advance(5 * time.Minute)
	store.Touch(key)

	updated := store.Get(key).LastActiveAt
	if !updated.After(original) {
		t.Errorf("LastActiveAt was not updated: original=%v, updated=%v", original, updated)
	}

	expected := original.Add(5 * time.Minute)
	if !updated.Equal(expected) {
		t.Errorf("LastActiveAt = %v, want %v", updated, expected)
	}
}

func TestInMemoryStore_Touch_NoOp(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore()
	key := SessionKey{Channel: "slack", ChatID: "missing"}

	// Touch on a missing key should not panic.
	store.Touch(key)

	if store.Len() != 0 {
		t.Errorf("Len() = %d, want 0 after touching missing key", store.Len())
	}
}

func TestInMemoryStore_Delete(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore()
	key := SessionKey{Channel: "slack", ChatID: "C1"}

	store.GetOrCreate(key)
	if store.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", store.Len())
	}

	store.Delete(key)

	if store.Len() != 0 {
		t.Errorf("Len() = %d, want 0 after delete", store.Len())
	}
	if got := store.Get(key); got != nil {
		t.Errorf("Get after Delete returned %v, want nil", got)
	}
}

func TestInMemoryStore_Prune(t *testing.T) {
	t.Parallel()

	store, ft := newTestStore()
	keyOld := SessionKey{Channel: "slack", ChatID: "old"}
	keyNew := SessionKey{Channel: "slack", ChatID: "new"}

	// Create "old" session first.
	store.GetOrCreate(keyOld)

	// Advance time, then create "new" session.
	ft.Advance(10 * time.Minute)
	store.GetOrCreate(keyNew)

	// Advance a bit more so "old" is past the threshold.
	ft.Advance(time.Minute)

	// Prune with 5-minute maxIdle: "old" is 11 min idle, "new" is 1 min idle.
	pruned := store.Prune(5 * time.Minute)

	if pruned != 1 {
		t.Errorf("Prune() = %d, want 1", pruned)
	}
	if store.Len() != 1 {
		t.Errorf("Len() = %d, want 1", store.Len())
	}
	if store.Get(keyOld) != nil {
		t.Error("old session should have been pruned")
	}
	if store.Get(keyNew) == nil {
		t.Error("new session should still exist")
	}
}

func TestInMemoryStore_Prune_NoneExpired(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore()
	key := SessionKey{Channel: "slack", ChatID: "C1"}
	store.GetOrCreate(key)

	pruned := store.Prune(time.Hour)
	if pruned != 0 {
		t.Errorf("Prune() = %d, want 0 when nothing expired", pruned)
	}
}

func TestInMemoryStore_Concurrent(t *testing.T) {
	t.Parallel()

	store, ft := newTestStore()
	keys := []SessionKey{
		{Channel: "a", ChatID: "1"},
		{Channel: "b", ChatID: "2"},
		{Channel: "c", ChatID: "3"},
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		key := keys[i%len(keys)]
		wg.Add(4)
		go func() {
			defer wg.Done()
			store.GetOrCreate(key)
		}()
		go func() {
			defer wg.Done()
			store.Touch(key)
		}()
		go func() {
			defer wg.Done()
			store.Get(key)
		}()
		go func() {
			defer wg.Done()
			ft.Advance(time.Millisecond)
			store.Len()
		}()
	}
	wg.Wait()

	// After all goroutines finish, verify store is consistent.
	if store.Len() > len(keys) {
		t.Errorf("Len() = %d, want <= %d", store.Len(), len(keys))
	}
}

func TestInMemoryStore_NilSlicesOnCreate(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore()
	key := SessionKey{Channel: "slack", ChatID: "C1"}

	sess, _ := store.GetOrCreate(key)

	// Idiomatic Go: nil slices and maps preferred over empty.
	if sess.History != nil {
		t.Error("History should be nil on creation")
	}
	if sess.Metadata != nil {
		t.Error("Metadata should be nil on creation")
	}
}

func TestInMemoryStore_ActiveKeys(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore()
	keyA := SessionKey{Channel: "slack", ChatID: "A"}
	keyB := SessionKey{Channel: "slack", ChatID: "B"}
	store.GetOrCreate(keyA)
	store.GetOrCreate(keyB)

	keys := store.ActiveKeys()
	if len(keys) != 2 {
		t.Fatalf("ActiveKeys() length = %d, want 2", len(keys))
	}
	if _, ok := keys[keyA]; !ok {
		t.Error("ActiveKeys() missing keyA")
	}
	if _, ok := keys[keyB]; !ok {
		t.Error("ActiveKeys() missing keyB")
	}

	// Ensure snapshot is independent from the store internals.
	delete(keys, keyA)
	if store.Len() != 2 {
		t.Errorf("Len() = %d, want 2 after mutating snapshot", store.Len())
	}
}
