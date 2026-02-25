package memory_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/memory"
)

// Compile-time interface guard.
var _ memory.Store = (*memory.InMemoryStore)(nil)

func testFact(id, content string) memory.Fact {
	return memory.Fact{
		ID:        id,
		Content:   content,
		Source:    "test",
		CreatedAt: time.Now(),
	}
}

func testFactWithMeta(id, content string, meta map[string]string) memory.Fact {
	f := testFact(id, content)
	f.Metadata = meta
	return f
}

func TestInMemoryStore_Index(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.NewInMemoryStore()

	facts := []memory.Fact{
		testFact("1", "first fact"),
		testFact("2", "second fact"),
		testFact("3", "third fact"),
	}

	for _, f := range facts {
		if err := store.Index(ctx, f); err != nil {
			t.Fatalf("Index(%q): unexpected error: %v", f.ID, err)
		}
	}

	if got := store.Len(); got != 3 {
		t.Fatalf("Len() = %d, want 3", got)
	}

	// Verify all facts are searchable.
	for _, f := range facts {
		results, err := store.Search(ctx, f.Content, 1)
		if err != nil {
			t.Fatalf("Search(%q): unexpected error: %v", f.Content, err)
		}
		if len(results) != 1 {
			t.Fatalf("Search(%q): got %d results, want 1", f.Content, len(results))
		}
		if results[0].ID != f.ID {
			t.Errorf("Search(%q)[0].ID = %q, want %q", f.Content, results[0].ID, f.ID)
		}
	}
}

func TestInMemoryStore_Index_UpdateExisting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.NewInMemoryStore()

	// Index a fact.
	if err := store.Index(ctx, testFact("1", "original content")); err != nil {
		t.Fatalf("Index: unexpected error: %v", err)
	}

	// Update the same ID with new content.
	if err := store.Index(ctx, testFact("1", "updated content")); err != nil {
		t.Fatalf("Index (update): unexpected error: %v", err)
	}

	// Len should still be 1.
	if got := store.Len(); got != 1 {
		t.Fatalf("Len() = %d, want 1", got)
	}

	// Search for updated content should find it.
	results, err := store.Search(ctx, "updated", 10)
	if err != nil {
		t.Fatalf("Search(updated): unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search(updated): got %d results, want 1", len(results))
	}
	if results[0].Content != "updated content" {
		t.Errorf("Content = %q, want %q", results[0].Content, "updated content")
	}

	// Search for original content should not find it.
	results, err = store.Search(ctx, "original", 10)
	if err != nil {
		t.Fatalf("Search(original): unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("Search(original): got %d results, want 0", len(results))
	}
}

func TestInMemoryStore_Search(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.NewInMemoryStore()

	facts := []memory.Fact{
		testFact("1", "Go is fast"),
		testFact("2", "Python is flexible"),
		testFact("3", "Go routines are great"),
	}
	for _, f := range facts {
		if err := store.Index(ctx, f); err != nil {
			t.Fatalf("Index(%q): unexpected error: %v", f.ID, err)
		}
	}

	tests := []struct {
		name    string
		query   string
		topK    int
		wantLen int
		wantNil bool
	}{
		{name: "substring match", query: "Go", topK: 10, wantLen: 2},
		{name: "case insensitive", query: "python", topK: 5, wantLen: 1},
		{name: "no matches", query: "rust", topK: 5, wantNil: true},
		{name: "topK limit", query: "Go", topK: 1, wantLen: 1},
		{name: "topK zero", query: "Go", topK: 0, wantNil: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			results, err := store.Search(ctx, tt.query, tt.topK)
			if err != nil {
				t.Fatalf("Search(%q, %d): unexpected error: %v", tt.query, tt.topK, err)
			}
			if tt.wantNil {
				if results != nil {
					t.Fatalf("Search(%q, %d): got %v, want nil", tt.query, tt.topK, results)
				}
				return
			}
			if len(results) != tt.wantLen {
				t.Fatalf("Search(%q, %d): got %d results, want %d", tt.query, tt.topK, len(results), tt.wantLen)
			}
		})
	}
}

func TestInMemoryStore_Search_TopK(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.NewInMemoryStore()

	for i, content := range []string{
		"test alpha",
		"test beta",
		"test gamma",
		"test delta",
		"test epsilon",
	} {
		id := string(rune('a' + i))
		if err := store.Index(ctx, testFact(id, content)); err != nil {
			t.Fatalf("Index(%q): unexpected error: %v", id, err)
		}
	}

	results, err := store.Search(ctx, "test", 2)
	if err != nil {
		t.Fatalf("Search(test, 2): unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Search(test, 2): got %d results, want 2", len(results))
	}
}

func TestInMemoryStore_SearchByMetadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.NewInMemoryStore()

	facts := []memory.Fact{
		testFactWithMeta("1", "Go is fast", map[string]string{"lang": "go"}),
		testFactWithMeta("2", "Python is flexible", map[string]string{"lang": "python"}),
		testFactWithMeta("3", "Go routines are great", map[string]string{"lang": "go"}),
	}
	for _, f := range facts {
		if err := store.Index(ctx, f); err != nil {
			t.Fatalf("Index(%q): unexpected error: %v", f.ID, err)
		}
	}

	tests := []struct {
		name    string
		key     string
		value   string
		wantLen int
		wantNil bool
	}{
		{name: "exact match", key: "lang", value: "go", wantLen: 2},
		{name: "single match", key: "lang", value: "python", wantLen: 1},
		{name: "no matches", key: "lang", value: "rust", wantNil: true},
		{name: "nonexistent key", key: "framework", value: "gin", wantNil: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			results, err := store.SearchByMetadata(ctx, tt.key, tt.value)
			if err != nil {
				t.Fatalf("SearchByMetadata(%q, %q): unexpected error: %v", tt.key, tt.value, err)
			}
			if tt.wantNil {
				if results != nil {
					t.Fatalf("SearchByMetadata(%q, %q): got %v, want nil", tt.key, tt.value, results)
				}
				return
			}
			if len(results) != tt.wantLen {
				t.Fatalf("SearchByMetadata(%q, %q): got %d results, want %d", tt.key, tt.value, len(results), tt.wantLen)
			}
			for _, r := range results {
				if r.Metadata[tt.key] != tt.value {
					t.Errorf("result %q has %s=%q, want %q", r.ID, tt.key, r.Metadata[tt.key], tt.value)
				}
			}
		})
	}
}

func TestInMemoryStore_Delete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.NewInMemoryStore()

	facts := []memory.Fact{
		testFact("1", "first fact"),
		testFact("2", "second fact"),
		testFact("3", "third fact"),
	}
	for _, f := range facts {
		if err := store.Index(ctx, f); err != nil {
			t.Fatalf("Index(%q): unexpected error: %v", f.ID, err)
		}
	}

	// Delete the middle fact.
	if err := store.Delete(ctx, "2"); err != nil {
		t.Fatalf("Delete(2): unexpected error: %v", err)
	}

	if got := store.Len(); got != 2 {
		t.Fatalf("Len() after delete = %d, want 2", got)
	}

	// Search should not find the deleted fact.
	results, err := store.Search(ctx, "second", 10)
	if err != nil {
		t.Fatalf("Search(second, 10): unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("Search(second, 10): got %d results, want 0", len(results))
	}

	// Remaining facts should still be searchable.
	results, err = store.Search(ctx, "first", 10)
	if err != nil {
		t.Fatalf("Search(first): unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search(first): got %d results, want 1", len(results))
	}

	results, err = store.Search(ctx, "third", 10)
	if err != nil {
		t.Fatalf("Search(third): unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search(third): got %d results, want 1", len(results))
	}
}

func TestInMemoryStore_Delete_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.NewInMemoryStore()

	err := store.Delete(ctx, "nonexistent")
	if err == nil {
		t.Fatal("Delete(nonexistent): expected error, got nil")
	}
	if !errors.Is(err, memory.ErrFactNotFound) {
		t.Fatalf("Delete(nonexistent): got %v, want %v", err, memory.ErrFactNotFound)
	}
}

func TestInMemoryStore_Len(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.NewInMemoryStore()

	// Initially 0.
	if got := store.Len(); got != 0 {
		t.Fatalf("Len() initially = %d, want 0", got)
	}

	// After indexing.
	for i := 0; i < 3; i++ {
		if err := store.Index(ctx, testFact(fmt.Sprintf("f%d", i), fmt.Sprintf("fact %d", i))); err != nil {
			t.Fatalf("Index: unexpected error: %v", err)
		}
	}
	if got := store.Len(); got != 3 {
		t.Fatalf("Len() after 3 indexes = %d, want 3", got)
	}

	// After deleting.
	if err := store.Delete(ctx, "f1"); err != nil {
		t.Fatalf("Delete: unexpected error: %v", err)
	}
	if got := store.Len(); got != 2 {
		t.Fatalf("Len() after delete = %d, want 2", got)
	}
}

func TestInMemoryStore_Concurrent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := memory.NewInMemoryStore()

	var wg sync.WaitGroup

	// Writers: index facts concurrently.
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(goroutine int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("g%d-f%d", goroutine, i)
				fact := testFact(id, fmt.Sprintf("goroutine %d fact %d content", goroutine, i))
				if err := store.Index(ctx, fact); err != nil {
					t.Errorf("Index from goroutine %d: unexpected error: %v", goroutine, err)
				}
			}
		}(g)
	}

	// Readers: search concurrently.
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func(goroutine int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				if _, err := store.Search(ctx, "content", 5); err != nil {
					t.Errorf("Search from goroutine %d: unexpected error: %v", goroutine, err)
				}
			}
		}(g)
	}

	wg.Wait()

	if got := store.Len(); got != 500 {
		t.Fatalf("Len() = %d, want 500", got)
	}
}
