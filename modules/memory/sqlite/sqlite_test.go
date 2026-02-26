package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/memory"
	"github.com/flemzord/sclaw/internal/provider"
)

func newTestModule(t *testing.T) *Module {
	t.Helper()

	dir := t.TempDir()
	m := &Module{
		config: Config{
			Path:        filepath.Join(dir, "test.db"),
			BusyTimeout: defaultBusyTimeout,
		},
	}
	m.config.defaults()

	ctx := core.NewAppContext(slog.Default(), dir, dir)

	if err := m.Provision(ctx); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	t.Cleanup(func() {
		_ = m.Stop(context.Background())
	})

	return m
}

// --- HistoryStore tests ---

func TestHistoryAppendAndGetAll(t *testing.T) {
	m := newTestModule(t)
	h := m.history

	msgs := []provider.LLMMessage{
		{Role: provider.MessageRoleUser, Content: "hello"},
		{Role: provider.MessageRoleAssistant, Content: "hi there"},
		{Role: provider.MessageRoleUser, Content: "how are you?"},
	}

	for _, msg := range msgs {
		if err := h.Append("s1", msg); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	got, err := h.GetAll("s1")
	if err != nil {
		t.Fatalf("get all: %v", err)
	}

	if len(got) != len(msgs) {
		t.Fatalf("got %d messages, want %d", len(got), len(msgs))
	}

	for i, msg := range got {
		if msg.Role != msgs[i].Role || msg.Content != msgs[i].Content {
			t.Errorf("message %d: got %+v, want %+v", i, msg, msgs[i])
		}
	}
}

func TestHistoryGetRecent(t *testing.T) {
	m := newTestModule(t)
	h := m.history

	for i := range 5 {
		msg := provider.LLMMessage{Role: provider.MessageRoleUser, Content: string(rune('a' + i))}
		if err := h.Append("s1", msg); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	got, err := h.GetRecent("s1", 3)
	if err != nil {
		t.Fatalf("get recent: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("got %d messages, want 3", len(got))
	}

	// Should be in chronological order: c, d, e.
	if got[0].Content != "c" || got[1].Content != "d" || got[2].Content != "e" {
		t.Errorf("got %v %v %v, want c d e", got[0].Content, got[1].Content, got[2].Content)
	}
}

func TestHistoryGetRecentMoreThanExists(t *testing.T) {
	m := newTestModule(t)
	h := m.history

	if err := h.Append("s1", provider.LLMMessage{Role: provider.MessageRoleUser, Content: "only"}); err != nil {
		t.Fatalf("append: %v", err)
	}

	got, err := h.GetRecent("s1", 100)
	if err != nil {
		t.Fatalf("get recent: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d messages, want 1", len(got))
	}
}

func TestHistoryEmptySession(t *testing.T) {
	m := newTestModule(t)
	h := m.history

	got, err := h.GetAll("nonexistent")
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}

	n, err := h.Len("nonexistent")
	if err != nil {
		t.Fatalf("len: %v", err)
	}
	if n != 0 {
		t.Errorf("len = %d, want 0", n)
	}

	summary, err := h.GetSummary("nonexistent")
	if err != nil {
		t.Fatalf("get summary: %v", err)
	}
	if summary != "" {
		t.Errorf("summary = %q, want empty", summary)
	}
}

func TestHistorySummary(t *testing.T) {
	m := newTestModule(t)
	h := m.history

	if err := h.SetSummary("s1", "first summary"); err != nil {
		t.Fatalf("set summary: %v", err)
	}

	got, err := h.GetSummary("s1")
	if err != nil {
		t.Fatalf("get summary: %v", err)
	}
	if got != "first summary" {
		t.Errorf("got %q, want %q", got, "first summary")
	}

	// Replace summary.
	if err := h.SetSummary("s1", "updated summary"); err != nil {
		t.Fatalf("set summary: %v", err)
	}

	got, err = h.GetSummary("s1")
	if err != nil {
		t.Fatalf("get summary: %v", err)
	}
	if got != "updated summary" {
		t.Errorf("got %q, want %q", got, "updated summary")
	}
}

func TestHistoryPurge(t *testing.T) {
	m := newTestModule(t)
	h := m.history

	if err := h.Append("s1", provider.LLMMessage{Role: provider.MessageRoleUser, Content: "hello"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := h.SetSummary("s1", "a summary"); err != nil {
		t.Fatalf("set summary: %v", err)
	}

	if err := h.Purge("s1"); err != nil {
		t.Fatalf("purge: %v", err)
	}

	n, err := h.Len("s1")
	if err != nil {
		t.Fatalf("len: %v", err)
	}
	if n != 0 {
		t.Errorf("len = %d after purge, want 0", n)
	}

	summary, err := h.GetSummary("s1")
	if err != nil {
		t.Fatalf("get summary: %v", err)
	}
	if summary != "" {
		t.Errorf("summary = %q after purge, want empty", summary)
	}
}

func TestHistoryLen(t *testing.T) {
	m := newTestModule(t)
	h := m.history

	n, err := h.Len("s1")
	if err != nil {
		t.Fatalf("len: %v", err)
	}
	if n != 0 {
		t.Errorf("len = %d, want 0", n)
	}

	if err := h.Append("s1", provider.LLMMessage{Role: provider.MessageRoleUser, Content: "a"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := h.Append("s1", provider.LLMMessage{Role: provider.MessageRoleAssistant, Content: "b"}); err != nil {
		t.Fatalf("append: %v", err)
	}

	n, err = h.Len("s1")
	if err != nil {
		t.Fatalf("len: %v", err)
	}
	if n != 2 {
		t.Errorf("len = %d, want 2", n)
	}
}

func TestHistoryToolCalls(t *testing.T) {
	m := newTestModule(t)
	h := m.history

	msg := provider.LLMMessage{
		Role:    provider.MessageRoleAssistant,
		Content: "",
		ToolCalls: []provider.ToolCall{
			{
				ID:        "call_1",
				Name:      "search",
				Arguments: json.RawMessage(`{"query":"test"}`),
			},
		},
	}

	if err := h.Append("s1", msg); err != nil {
		t.Fatalf("append: %v", err)
	}

	got, err := h.GetAll("s1")
	if err != nil {
		t.Fatalf("get all: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d messages, want 1", len(got))
	}
	if len(got[0].ToolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(got[0].ToolCalls))
	}
	if got[0].ToolCalls[0].ID != "call_1" || got[0].ToolCalls[0].Name != "search" {
		t.Errorf("tool call mismatch: %+v", got[0].ToolCalls[0])
	}
}

func TestHistoryIsError(t *testing.T) {
	m := newTestModule(t)
	h := m.history

	msg := provider.LLMMessage{
		Role:    provider.MessageRoleTool,
		Content: "error output",
		ToolID:  "call_1",
		IsError: true,
	}

	if err := h.Append("s1", msg); err != nil {
		t.Fatalf("append: %v", err)
	}

	got, err := h.GetAll("s1")
	if err != nil {
		t.Fatalf("get all: %v", err)
	}

	if !got[0].IsError {
		t.Error("expected IsError=true")
	}
	if got[0].ToolID != "call_1" {
		t.Errorf("ToolID = %q, want %q", got[0].ToolID, "call_1")
	}
}

// --- Store tests ---

func TestStoreIndexAndSearch(t *testing.T) {
	m := newTestModule(t)
	s := m.store
	ctx := context.Background()

	facts := []memory.Fact{
		{ID: "f1", Content: "Go is a compiled programming language", Source: "s1", Tags: []string{"go", "lang"}},
		{ID: "f2", Content: "Python is an interpreted programming language", Source: "s1", Tags: []string{"python"}},
		{ID: "f3", Content: "SQLite is an embedded database", Source: "s2", Metadata: map[string]string{"type": "database"}},
	}

	for _, fact := range facts {
		if err := s.Index(ctx, fact); err != nil {
			t.Fatalf("index: %v", err)
		}
	}

	// Search for "programming" should return f1 and f2.
	results, err := s.Search(ctx, "programming", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	// Search for "database" should return f3.
	results, err = s.Search(ctx, "database", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].ID != "f3" {
		t.Errorf("got ID %q, want %q", results[0].ID, "f3")
	}
}

func TestStoreSearchTopK(t *testing.T) {
	m := newTestModule(t)
	s := m.store
	ctx := context.Background()

	for i := range 5 {
		if err := s.Index(ctx, memory.Fact{
			ID:      fmt.Sprintf("f%d", i),
			Content: fmt.Sprintf("test document number %d", i),
		}); err != nil {
			t.Fatalf("index: %v", err)
		}
	}

	results, err := s.Search(ctx, "document", 3)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("got %d results, want 3", len(results))
	}
}

func TestStoreSearchByMetadata(t *testing.T) {
	m := newTestModule(t)
	s := m.store
	ctx := context.Background()

	facts := []memory.Fact{
		{ID: "f1", Content: "fact one", Metadata: map[string]string{"author": "alice"}},
		{ID: "f2", Content: "fact two", Metadata: map[string]string{"author": "bob"}},
		{ID: "f3", Content: "fact three", Metadata: map[string]string{"author": "alice", "status": "reviewed"}},
	}

	for _, fact := range facts {
		if err := s.Index(ctx, fact); err != nil {
			t.Fatalf("index: %v", err)
		}
	}

	results, err := s.SearchByMetadata(ctx, "author", "alice")
	if err != nil {
		t.Fatalf("search by metadata: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	// No matches.
	results, err = s.SearchByMetadata(ctx, "author", "charlie")
	if err != nil {
		t.Fatalf("search by metadata: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestStoreDelete(t *testing.T) {
	m := newTestModule(t)
	s := m.store
	ctx := context.Background()

	if err := s.Index(ctx, memory.Fact{ID: "f1", Content: "to be deleted"}); err != nil {
		t.Fatalf("index: %v", err)
	}

	if err := s.Delete(ctx, "f1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if s.Len() != 0 {
		t.Errorf("len = %d after delete, want 0", s.Len())
	}

	// FTS should also be cleaned up.
	results, err := s.Search(ctx, "deleted", 10)
	if err != nil {
		t.Fatalf("search after delete: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d search results after delete, want 0", len(results))
	}
}

func TestStoreDeleteNotFound(t *testing.T) {
	m := newTestModule(t)

	err := m.store.Delete(context.Background(), "nonexistent")
	if !errors.Is(err, memory.ErrFactNotFound) {
		t.Errorf("got error %v, want %v", err, memory.ErrFactNotFound)
	}
}

func TestStoreUpsert(t *testing.T) {
	m := newTestModule(t)
	s := m.store
	ctx := context.Background()

	if err := s.Index(ctx, memory.Fact{ID: "f1", Content: "original content"}); err != nil {
		t.Fatalf("index: %v", err)
	}

	// Update the same fact.
	if err := s.Index(ctx, memory.Fact{ID: "f1", Content: "updated content"}); err != nil {
		t.Fatalf("index update: %v", err)
	}

	if s.Len() != 1 {
		t.Errorf("len = %d after upsert, want 1", s.Len())
	}

	// FTS should reflect the updated content.
	results, err := s.Search(ctx, "updated", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Content != "updated content" {
		t.Errorf("content = %q, want %q", results[0].Content, "updated content")
	}

	// Old content should not be searchable.
	results, err = s.Search(ctx, "original", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results for old content, want 0", len(results))
	}
}

func TestStoreLen(t *testing.T) {
	m := newTestModule(t)
	s := m.store
	ctx := context.Background()

	if s.Len() != 0 {
		t.Errorf("len = %d, want 0", s.Len())
	}

	for i := range 3 {
		if err := s.Index(ctx, memory.Fact{
			ID:      fmt.Sprintf("f%d", i),
			Content: "test",
		}); err != nil {
			t.Fatalf("index: %v", err)
		}
	}

	if s.Len() != 3 {
		t.Errorf("len = %d, want 3", s.Len())
	}
}

func TestStorePreservesFields(t *testing.T) {
	m := newTestModule(t)
	s := m.store
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	fact := memory.Fact{
		ID:        "f1",
		Content:   "test content",
		Source:    "session-42",
		Tags:      []string{"tag1", "tag2"},
		Metadata:  map[string]string{"key": "value"},
		CreatedAt: now,
	}

	if err := s.Index(ctx, fact); err != nil {
		t.Fatalf("index: %v", err)
	}

	results, err := s.Search(ctx, "test", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	got := results[0]
	if got.ID != fact.ID {
		t.Errorf("ID = %q, want %q", got.ID, fact.ID)
	}
	if got.Source != fact.Source {
		t.Errorf("Source = %q, want %q", got.Source, fact.Source)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "tag1" || got.Tags[1] != "tag2" {
		t.Errorf("Tags = %v, want %v", got.Tags, fact.Tags)
	}
	if got.Metadata["key"] != "value" {
		t.Errorf("Metadata = %v, want %v", got.Metadata, fact.Metadata)
	}
	if !got.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, now)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	m := newTestModule(t)

	results, err := m.store.Search(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if results != nil {
		t.Errorf("got %v, want nil", results)
	}
}

// --- Concurrency tests ---

func TestConcurrentAppend(t *testing.T) {
	m := newTestModule(t)
	h := m.history

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			msg := provider.LLMMessage{
				Role:    provider.MessageRoleUser,
				Content: fmt.Sprintf("message %d", i),
			}
			if err := h.Append("s1", msg); err != nil {
				t.Errorf("concurrent append: %v", err)
			}
		}()
	}
	wg.Wait()

	n, err := h.Len("s1")
	if err != nil {
		t.Fatalf("len: %v", err)
	}
	if n != 10 {
		t.Errorf("len = %d, want 10", n)
	}
}

func TestConcurrentIndexAndSearch(t *testing.T) {
	m := newTestModule(t)
	s := m.store
	ctx := context.Background()

	var wg sync.WaitGroup

	// Writers.
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.Index(ctx, memory.Fact{
				ID:      fmt.Sprintf("fact-%d", i),
				Content: fmt.Sprintf("concurrent document %d", i),
			})
			if err != nil {
				t.Errorf("concurrent index: %v", err)
			}
		}()
	}

	// Readers (run concurrently with writers).
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Search(ctx, "concurrent", 10)
			if err != nil {
				t.Errorf("concurrent search: %v", err)
			}
		}()
	}

	wg.Wait()

	if s.Len() != 10 {
		t.Errorf("len = %d, want 10", s.Len())
	}
}

// --- Infrastructure tests ---

func TestWALMode(t *testing.T) {
	m := newTestModule(t)

	var mode string
	if err := m.db.QueryRowContext(context.TODO(), "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("pragma journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want %q", mode, "wal")
	}
}

func TestMigrationIdempotent(t *testing.T) {
	m := newTestModule(t)

	// Run migration again — should be a no-op.
	if err := migrate(m.db); err != nil {
		t.Fatalf("second migration: %v", err)
	}

	// Verify tables still work.
	if err := m.history.Append("s1", provider.LLMMessage{Role: provider.MessageRoleUser, Content: "test"}); err != nil {
		t.Fatalf("append after re-migration: %v", err)
	}
}

func TestMultipleSessions(t *testing.T) {
	m := newTestModule(t)
	h := m.history

	if err := h.Append("s1", provider.LLMMessage{Role: provider.MessageRoleUser, Content: "s1-msg"}); err != nil {
		t.Fatalf("append s1: %v", err)
	}
	if err := h.Append("s2", provider.LLMMessage{Role: provider.MessageRoleUser, Content: "s2-msg"}); err != nil {
		t.Fatalf("append s2: %v", err)
	}

	n1, _ := h.Len("s1")
	n2, _ := h.Len("s2")
	if n1 != 1 || n2 != 1 {
		t.Errorf("s1=%d s2=%d, want 1 and 1", n1, n2)
	}

	// Purge s1, s2 should be unaffected.
	if err := h.Purge("s1"); err != nil {
		t.Fatalf("purge: %v", err)
	}

	n1, _ = h.Len("s1")
	n2, _ = h.Len("s2")
	if n1 != 0 || n2 != 1 {
		t.Errorf("after purge: s1=%d s2=%d, want 0 and 1", n1, n2)
	}
}
