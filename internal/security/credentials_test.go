package security

import (
	"context"
	"sync"
	"testing"
)

func TestCredentialStore_SetGet(t *testing.T) {
	t.Parallel()

	store := NewCredentialStore()
	store.Set("openai", "sk-test123")

	val, ok := store.Get("openai")
	if !ok {
		t.Fatal("expected credential to exist")
	}
	if val != "sk-test123" {
		t.Fatalf("got %q, want %q", val, "sk-test123")
	}
}

func TestCredentialStore_GetMissing(t *testing.T) {
	t.Parallel()

	store := NewCredentialStore()
	_, ok := store.Get("missing")
	if ok {
		t.Fatal("expected missing credential to return false")
	}
}

func TestCredentialStore_Has(t *testing.T) {
	t.Parallel()

	store := NewCredentialStore()
	store.Set("key", "value")

	if !store.Has("key") {
		t.Fatal("expected Has to return true for existing key")
	}
	if store.Has("missing") {
		t.Fatal("expected Has to return false for missing key")
	}
}

func TestCredentialStore_Overwrite(t *testing.T) {
	t.Parallel()

	store := NewCredentialStore()
	store.Set("key", "v1")
	store.Set("key", "v2")

	val, _ := store.Get("key")
	if val != "v2" {
		t.Fatalf("got %q, want %q", val, "v2")
	}
	if store.Len() != 1 {
		t.Fatalf("got len %d, want 1", store.Len())
	}
}

func TestCredentialStore_Names(t *testing.T) {
	t.Parallel()

	store := NewCredentialStore()
	store.Set("zulu", "z")
	store.Set("alpha", "a")
	store.Set("mike", "m")

	names := store.Names()
	want := []string{"alpha", "mike", "zulu"}
	if len(names) != len(want) {
		t.Fatalf("got %d names, want %d", len(names), len(want))
	}
	for i, name := range names {
		if name != want[i] {
			t.Errorf("names[%d] = %q, want %q", i, name, want[i])
		}
	}
}

func TestCredentialStore_Values(t *testing.T) {
	t.Parallel()

	store := NewCredentialStore()
	store.Set("a", "val-a")
	store.Set("b", "") // empty values are excluded
	store.Set("c", "val-c")

	values := store.Values()
	if len(values) != 2 {
		t.Fatalf("got %d values, want 2 (empty excluded)", len(values))
	}
}

func TestCredentialStore_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	store := NewCredentialStore()
	var wg sync.WaitGroup

	// Concurrent writes.
	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store.Set("key", "value")
			store.Get("key")
			store.Has("key")
			store.Names()
			store.Values()
			store.Len()
			_ = i
		}(i)
	}
	wg.Wait()
}

func TestWithCredentials_RoundTrip(t *testing.T) {
	t.Parallel()

	store := NewCredentialStore()
	store.Set("test", "secret")

	ctx := WithCredentials(context.Background(), store)
	got := CredentialsFromContext(ctx)

	if got == nil {
		t.Fatal("expected store from context, got nil")
	}
	if !got.Has("test") {
		t.Fatal("expected credential 'test' to exist in retrieved store")
	}
}

func TestCredentialsFromContext_Missing(t *testing.T) {
	t.Parallel()

	got := CredentialsFromContext(context.Background())
	if got != nil {
		t.Fatal("expected nil from empty context")
	}
}
