package mcp

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestResolver_NoMCPJSON(t *testing.T) {
	r := NewResolver(slog.Default())
	defer r.Close()

	tools := r.ResolveTools(context.Background(), "agent1", t.TempDir())
	if len(tools) != 0 {
		t.Fatalf("expected no tools, got %d", len(tools))
	}
}

func TestResolver_EmptyDataDir(t *testing.T) {
	r := NewResolver(slog.Default())
	defer r.Close()

	tools := r.ResolveTools(context.Background(), "agent1", "")
	if tools != nil {
		t.Fatal("expected nil for empty dataDir")
	}
}

func TestResolver_CachesNil(t *testing.T) {
	r := NewResolver(slog.Default())
	defer r.Close()

	dir := t.TempDir()

	// First call: reads filesystem.
	tools1 := r.ResolveTools(context.Background(), "agent1", dir)
	// Second call: should hit cache.
	tools2 := r.ResolveTools(context.Background(), "agent1", dir)

	if len(tools1) != 0 || len(tools2) != 0 {
		t.Fatal("expected no tools")
	}

	// Verify cache entry exists.
	r.mu.RLock()
	_, ok := r.agents["agent1"]
	r.mu.RUnlock()

	if !ok {
		t.Fatal("expected agent1 to be cached")
	}
}

func TestResolver_InvalidateAgent(t *testing.T) {
	r := NewResolver(slog.Default())
	defer r.Close()

	dir := t.TempDir()
	r.ResolveTools(context.Background(), "agent1", dir)

	// Verify cached.
	r.mu.RLock()
	_, ok := r.agents["agent1"]
	r.mu.RUnlock()
	if !ok {
		t.Fatal("expected agent1 to be cached before invalidation")
	}

	r.InvalidateAgent("agent1")

	// Verify removed.
	r.mu.RLock()
	_, ok = r.agents["agent1"]
	r.mu.RUnlock()
	if ok {
		t.Fatal("expected agent1 to be removed after invalidation")
	}
}

func TestResolver_InvalidateNonExistent(t *testing.T) {
	r := NewResolver(slog.Default())
	defer r.Close()

	// Should not panic.
	r.InvalidateAgent("nonexistent")
}

func TestResolver_Close(t *testing.T) {
	r := NewResolver(slog.Default())

	dir := t.TempDir()
	r.ResolveTools(context.Background(), "agent1", dir)
	r.ResolveTools(context.Background(), "agent2", dir)

	if err := r.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	// Verify all agents cleared.
	r.mu.RLock()
	count := len(r.agents)
	r.mu.RUnlock()
	if count != 0 {
		t.Fatalf("expected 0 agents after Close, got %d", count)
	}
}

func TestResolver_InvalidConfigSkipped(t *testing.T) {
	r := NewResolver(slog.Default())
	defer r.Close()

	dir := t.TempDir()
	// Write invalid JSON.
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte("{bad}"), 0o644); err != nil {
		t.Fatal(err)
	}

	tools := r.ResolveTools(context.Background(), "agent1", dir)
	if len(tools) != 0 {
		t.Fatalf("expected no tools for invalid config, got %d", len(tools))
	}
}
