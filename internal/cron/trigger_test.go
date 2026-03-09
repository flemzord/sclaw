package cron

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCronTrigger_List_Empty(t *testing.T) {
	t.Parallel()
	ct := NewTrigger()
	infos := ct.List()
	if len(infos) != 0 {
		t.Errorf("List() = %d items, want 0", len(infos))
	}
}

func TestCronTrigger_Register_And_List(t *testing.T) {
	t.Parallel()
	ct := NewTrigger()

	ct.Register(&PromptJob{
		Def:     PromptCronDef{Name: "beta", Schedule: "0 8 * * *", Enabled: true},
		AgentID: "main",
		DataDir: t.TempDir(),
	})
	ct.Register(&PromptJob{
		Def:     PromptCronDef{Name: "alpha", Schedule: "0 7 * * *", Enabled: false},
		AgentID: "other",
		DataDir: t.TempDir(),
	})

	infos := ct.List()
	if len(infos) != 2 {
		t.Fatalf("List() = %d items, want 2", len(infos))
	}
	// Sorted by name.
	if infos[0].Name != "alpha" {
		t.Errorf("infos[0].Name = %q, want %q", infos[0].Name, "alpha")
	}
	if infos[1].Name != "beta" {
		t.Errorf("infos[1].Name = %q, want %q", infos[1].Name, "beta")
	}
	if infos[0].AgentID != "other" {
		t.Errorf("infos[0].AgentID = %q, want %q", infos[0].AgentID, "other")
	}
}

func TestCronTrigger_Get_Found(t *testing.T) {
	t.Parallel()
	ct := NewTrigger()
	ct.Register(&PromptJob{
		Def:     PromptCronDef{Name: "test-cron", Schedule: "*/5 * * * *", Enabled: true, Description: "a test"},
		AgentID: "main",
		DataDir: t.TempDir(),
	})

	info, ok := ct.Get("test-cron")
	if !ok {
		t.Fatal("Get() returned false, want true")
	}
	if info.Name != "test-cron" {
		t.Errorf("Name = %q, want %q", info.Name, "test-cron")
	}
	if info.Description != "a test" {
		t.Errorf("Description = %q, want %q", info.Description, "a test")
	}
}

func TestCronTrigger_Get_NotFound(t *testing.T) {
	t.Parallel()
	ct := NewTrigger()
	_, ok := ct.Get("nonexistent")
	if ok {
		t.Error("Get() returned true for nonexistent cron")
	}
}

func TestCronTrigger_Get_WithLastResult(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	resultsDir := filepath.Join(dataDir, "crons", "results")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	result := PromptCronResult{
		Name:       "my-cron",
		RanAt:      "2026-01-01T00:00:00Z",
		DurationMs: 1234,
		StopReason: "complete",
		Content:    "hello world",
	}
	data, _ := json.Marshal(result)
	if err := os.WriteFile(filepath.Join(resultsDir, "my-cron.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	ct := NewTrigger()
	ct.Register(&PromptJob{
		Def:     PromptCronDef{Name: "my-cron", Schedule: "0 9 * * *", Enabled: true},
		AgentID: "main",
		DataDir: dataDir,
	})

	info, ok := ct.Get("my-cron")
	if !ok {
		t.Fatal("Get() returned false")
	}
	if info.LastResult == nil {
		t.Fatal("LastResult is nil, want non-nil")
	}
	if info.LastResult.Content != "hello world" {
		t.Errorf("LastResult.Content = %q, want %q", info.LastResult.Content, "hello world")
	}
}

func TestCronTrigger_Trigger_NotFound(t *testing.T) {
	t.Parallel()
	ct := NewTrigger()
	err := ct.Trigger(context.Background(), "nonexistent")
	if err == nil {
		t.Error("Trigger() returned nil, want error")
	}
}

func TestLoadResult_NotFound(t *testing.T) {
	t.Parallel()
	_, err := LoadResult(t.TempDir(), "nonexistent")
	if err == nil {
		t.Error("LoadResult() returned nil, want error")
	}
}

func TestLoadResult_Valid(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	result := PromptCronResult{
		Name:       "test",
		DurationMs: 500,
		StopReason: "complete",
		Content:    "result content",
	}
	if err := SaveResult(dataDir, result); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadResult(dataDir, "test")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Content != "result content" {
		t.Errorf("Content = %q, want %q", loaded.Content, "result content")
	}
	if loaded.DurationMs != 500 {
		t.Errorf("DurationMs = %d, want 500", loaded.DurationMs)
	}
}
