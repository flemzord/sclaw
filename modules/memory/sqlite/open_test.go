package sqlite_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/flemzord/sclaw/internal/memory"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/modules/memory/sqlite"
)

func TestOpenHistoryStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	store, db, err := sqlite.OpenHistoryStore(dbPath)
	if err != nil {
		t.Fatalf("OpenHistoryStore: %v", err)
	}
	defer func() { _ = db.Close() }()

	if store == nil {
		t.Fatal("expected non-nil store")
	}

	sessionID := "test-session"

	// Append two messages.
	if err := store.Append(sessionID, provider.LLMMessage{
		Role:    provider.MessageRoleUser,
		Content: "hello",
	}); err != nil {
		t.Fatalf("Append user: %v", err)
	}
	if err := store.Append(sessionID, provider.LLMMessage{
		Role:    provider.MessageRoleAssistant,
		Content: "hi there",
	}); err != nil {
		t.Fatalf("Append assistant: %v", err)
	}

	// GetRecent should return both in chronological order.
	msgs, err := store.GetRecent(sessionID, 10)
	if err != nil {
		t.Fatalf("GetRecent: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != provider.MessageRoleUser || msgs[0].Content != "hello" {
		t.Errorf("msg[0] = %+v, want user/hello", msgs[0])
	}
	if msgs[1].Role != provider.MessageRoleAssistant || msgs[1].Content != "hi there" {
		t.Errorf("msg[1] = %+v, want assistant/hi there", msgs[1])
	}
}

func TestOpenHistoryStore_CreatesDirectory(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "nested", "dir", "test.db")

	store, db, err := sqlite.OpenHistoryStore(dbPath)
	if err != nil {
		t.Fatalf("OpenHistoryStore: %v", err)
	}
	defer func() { _ = db.Close() }()

	if store == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestOpenStores(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	histStore, factStore, db, err := sqlite.OpenStores(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("OpenStores: %v", err)
	}
	defer func() { _ = db.Close() }()

	if histStore == nil {
		t.Fatal("expected non-nil history store")
	}
	if factStore == nil {
		t.Fatal("expected non-nil fact store")
	}

	// Verify history store works.
	if err := histStore.Append("sess1", provider.LLMMessage{
		Role:    provider.MessageRoleUser,
		Content: "hello",
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	msgs, err := histStore.GetRecent("sess1", 10)
	if err != nil {
		t.Fatalf("GetRecent: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	// Verify fact store works.
	if err := factStore.Index(context.Background(), memory.Fact{
		ID:      "f1",
		Content: "test fact content",
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}

	results, err := factStore.Search(context.Background(), "test", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "f1" {
		t.Errorf("got ID %q, want %q", results[0].ID, "f1")
	}
}

func TestOpenStores_CreatesDirectory(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "nested", "dir", "test.db")

	histStore, factStore, db, err := sqlite.OpenStores(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("OpenStores: %v", err)
	}
	defer func() { _ = db.Close() }()

	if histStore == nil {
		t.Fatal("expected non-nil history store")
	}
	if factStore == nil {
		t.Fatal("expected non-nil fact store")
	}
}
