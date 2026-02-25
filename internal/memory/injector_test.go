package memory_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/memory"
)

// mockEstimator implements ctxengine.TokenEstimator for tests.
type mockEstimator struct{}

func (mockEstimator) Estimate(text string) int { return len(text)/4 + 1 }

func seedStore(t *testing.T, store *memory.InMemoryStore, facts []memory.Fact) {
	t.Helper()
	ctx := context.Background()
	for _, f := range facts {
		if err := store.Index(ctx, f); err != nil {
			t.Fatalf("Index(%q): unexpected error: %v", f.ID, err)
		}
	}
}

func TestInjectMemory_WithFacts(t *testing.T) {
	t.Parallel()

	store := memory.NewInMemoryStore()
	ctx := context.Background()
	now := time.Now()

	facts := []memory.Fact{
		{ID: "f1", Content: "user likes golang", CreatedAt: now},
		{ID: "f2", Content: "user prefers dark mode", CreatedAt: now},
		{ID: "f3", Content: "user likes rust too", CreatedAt: now},
	}
	seedStore(t, store, facts)

	est := mockEstimator{}
	// Large token budget so all matching facts are returned.
	result, err := memory.InjectMemory(ctx, store, "likes", 10, 10000, est)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("got %d facts, want 2 (matching 'likes')", len(result))
	}

	// Verify the returned strings contain the expected content.
	joined := strings.Join(result, " ")
	if !strings.Contains(joined, "user likes golang") {
		t.Errorf("expected result to contain %q", "user likes golang")
	}
	if !strings.Contains(joined, "user likes rust too") {
		t.Errorf("expected result to contain %q", "user likes rust too")
	}
}

func TestInjectMemory_TokenBudgetTruncates(t *testing.T) {
	t.Parallel()

	store := memory.NewInMemoryStore()
	ctx := context.Background()
	now := time.Now()

	// Index 10 facts with long content that all match "data".
	var facts []memory.Fact
	for i := 0; i < 10; i++ {
		facts = append(facts, memory.Fact{
			ID:        fmt.Sprintf("f%d", i),
			Content:   fmt.Sprintf("data point %d with a lot of extra content to consume tokens padding padding padding", i),
			CreatedAt: now,
		})
	}
	seedStore(t, store, facts)

	est := mockEstimator{}

	// Full result with large budget.
	fullResult, err := memory.InjectMemory(ctx, store, "data", 10, 100000, est)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Limited result with small token budget.
	// Each fact content is ~80 chars → ~21 tokens each. Budget of 30 should allow ~1 fact.
	limitedResult, err := memory.InjectMemory(ctx, store, "data", 10, 30, est)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(limitedResult) >= len(fullResult) {
		t.Errorf("expected limited result (%d facts) to have fewer facts than full result (%d facts)",
			len(limitedResult), len(fullResult))
	}
	if len(limitedResult) == 0 {
		t.Error("expected at least 1 fact in limited result")
	}
}

func TestInjectMemory_NilStore(t *testing.T) {
	t.Parallel()

	est := mockEstimator{}
	result, err := memory.InjectMemory(context.Background(), nil, "query", 10, 1000, est)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("got %v, want nil", result)
	}
}

func TestInjectMemory_EmptyQuery(t *testing.T) {
	t.Parallel()

	store := memory.NewInMemoryStore()
	ctx := context.Background()

	// Index a fact with content that contains empty string (everything matches).
	seedStore(t, store, []memory.Fact{
		{ID: "f1", Content: "user likes golang", CreatedAt: time.Now()},
	})

	est := mockEstimator{}
	result, err := memory.InjectMemory(ctx, store, "", 10, 10000, est)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty string is a substring of everything, so we should get results.
	if len(result) != 1 {
		t.Fatalf("got %d facts, want 1", len(result))
	}
}

func TestInjectMemory_MaxFactsZero(t *testing.T) {
	t.Parallel()

	store := memory.NewInMemoryStore()
	ctx := context.Background()
	seedStore(t, store, []memory.Fact{
		{ID: "f1", Content: "user likes golang", CreatedAt: time.Now()},
	})

	est := mockEstimator{}
	result, err := memory.InjectMemory(ctx, store, "likes", 0, 10000, est)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("got %v, want nil", result)
	}
}

func TestInjectMemory_MaxTokensZero(t *testing.T) {
	t.Parallel()

	store := memory.NewInMemoryStore()
	ctx := context.Background()
	seedStore(t, store, []memory.Fact{
		{ID: "f1", Content: "user likes golang", CreatedAt: time.Now()},
	})

	est := mockEstimator{}
	result, err := memory.InjectMemory(ctx, store, "likes", 10, 0, est)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("got %v, want nil", result)
	}
}

func TestFormatFacts_Empty(t *testing.T) {
	t.Parallel()

	result := memory.FormatFacts(nil)
	if result != "" {
		t.Fatalf("FormatFacts(nil) = %q, want empty string", result)
	}

	result = memory.FormatFacts([]string{})
	if result != "" {
		t.Fatalf("FormatFacts([]) = %q, want empty string", result)
	}
}

func TestFormatFacts_FormatsCorrectly(t *testing.T) {
	t.Parallel()

	facts := []string{"user likes golang", "user prefers dark mode"}
	result := memory.FormatFacts(facts)

	if !strings.Contains(result, "## Relevant Memory") {
		t.Errorf("expected header '## Relevant Memory', got %q", result)
	}
	if !strings.Contains(result, "- user likes golang\n") {
		t.Errorf("expected bullet '- user likes golang', got %q", result)
	}
	if !strings.Contains(result, "- user prefers dark mode\n") {
		t.Errorf("expected bullet '- user prefers dark mode', got %q", result)
	}

	// Verify exact format.
	want := "## Relevant Memory\n\n- user likes golang\n- user prefers dark mode\n"
	if result != want {
		t.Errorf("FormatFacts:\ngot:  %q\nwant: %q", result, want)
	}
}
