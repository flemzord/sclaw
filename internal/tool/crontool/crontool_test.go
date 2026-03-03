package crontool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/flemzord/sclaw/internal/cron"
	"github.com/flemzord/sclaw/internal/tool"
)

// testEnv creates a tool.ExecutionEnv with a temporary data directory.
func testEnv(t *testing.T) (tool.ExecutionEnv, string) {
	t.Helper()
	dir := t.TempDir()
	return tool.ExecutionEnv{DataDir: dir}, dir
}

// writeCronDef writes a cron definition to the crons directory.
func writeCronDef(t *testing.T, dataDir string, def cron.PromptCronDef) {
	t.Helper()
	dir := cron.CronsDir(dataDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating crons dir: %v", err)
	}
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshaling def: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, def.Name+".json"), data, 0o644); err != nil {
		t.Fatalf("writing def file: %v", err)
	}
}

func TestCRUD_CreateListGetDeleteFlow(t *testing.T) {
	env, dataDir := testEnv(t)

	reloadCalled := 0
	deps := Deps{ReloadFn: func() error { reloadCalled++; return nil }}

	createT := newCreateTool(deps)
	listT := newListTool()
	getT := newGetTool()
	deleteT := newDeleteTool(deps)

	// Create a cron.
	createArgs, _ := json.Marshal(map[string]any{
		"name":     "test-cron",
		"schedule": "0 9 * * *",
		"prompt":   "analyze tools",
		"enabled":  true,
	})
	out, err := createT.Execute(context.Background(), createArgs, env)
	if err != nil {
		t.Fatalf("create: unexpected error: %v", err)
	}
	if out.IsError {
		t.Fatalf("create: unexpected tool error: %s", out.Content)
	}
	if reloadCalled != 1 {
		t.Errorf("reload called %d times, want 1", reloadCalled)
	}

	// Verify the file was written.
	defPath := filepath.Join(cron.CronsDir(dataDir), "test-cron.json")
	if _, err := os.Stat(defPath); os.IsNotExist(err) {
		t.Fatal("cron file not created")
	}

	// List should show the cron.
	listOut, err := listT.Execute(context.Background(), json.RawMessage(`{}`), env)
	if err != nil {
		t.Fatalf("list: unexpected error: %v", err)
	}
	var entries []map[string]any
	if err := json.Unmarshal([]byte(listOut.Content), &entries); err != nil {
		t.Fatalf("list: unmarshal error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("list: expected 1 entry, got %d", len(entries))
	}
	if entries[0]["name"] != "test-cron" {
		t.Errorf("list: got name %v, want test-cron", entries[0]["name"])
	}

	// Get should return the definition.
	getArgs, _ := json.Marshal(map[string]string{"name": "test-cron"})
	getOut, err := getT.Execute(context.Background(), getArgs, env)
	if err != nil {
		t.Fatalf("get: unexpected error: %v", err)
	}
	if getOut.IsError {
		t.Fatalf("get: unexpected tool error: %s", getOut.Content)
	}

	// Delete should remove the file.
	deleteArgs, _ := json.Marshal(map[string]string{"name": "test-cron"})
	deleteOut, err := deleteT.Execute(context.Background(), deleteArgs, env)
	if err != nil {
		t.Fatalf("delete: unexpected error: %v", err)
	}
	if deleteOut.IsError {
		t.Fatalf("delete: unexpected tool error: %s", deleteOut.Content)
	}
	if reloadCalled != 2 {
		t.Errorf("reload called %d times after delete, want 2", reloadCalled)
	}
	if _, err := os.Stat(defPath); !os.IsNotExist(err) {
		t.Error("cron file not deleted")
	}
}

func TestCreate_DuplicateRejected(t *testing.T) {
	env, dataDir := testEnv(t)

	writeCronDef(t, dataDir, cron.PromptCronDef{
		Name: "existing", Schedule: "* * * * *", Prompt: "hello", Enabled: true,
	})

	deps := Deps{ReloadFn: func() error { return nil }}
	createT := newCreateTool(deps)

	args, _ := json.Marshal(map[string]any{
		"name": "existing", "schedule": "* * * * *", "prompt": "hello",
	})
	out, err := createT.Execute(context.Background(), args, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.IsError {
		t.Error("expected error for duplicate cron")
	}
}

func TestCreate_InvalidNameRejected(t *testing.T) {
	env, _ := testEnv(t)
	deps := Deps{ReloadFn: func() error { return nil }}
	createT := newCreateTool(deps)

	args, _ := json.Marshal(map[string]any{
		"name": "bad name!", "schedule": "* * * * *", "prompt": "hello",
	})
	out, err := createT.Execute(context.Background(), args, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.IsError {
		t.Error("expected error for invalid name")
	}
}

func TestUpdate_SelectiveFields(t *testing.T) {
	env, dataDir := testEnv(t)

	writeCronDef(t, dataDir, cron.PromptCronDef{
		Name: "mycron", Schedule: "0 9 * * *", Prompt: "original", Enabled: false,
	})

	deps := Deps{ReloadFn: func() error { return nil }}
	updateT := newUpdateTool(deps)

	args, _ := json.Marshal(map[string]any{
		"name":    "mycron",
		"enabled": true,
		"prompt":  "updated prompt",
	})
	out, err := updateT.Execute(context.Background(), args, env)
	if err != nil {
		t.Fatalf("update: unexpected error: %v", err)
	}
	if out.IsError {
		t.Fatalf("update: unexpected tool error: %s", out.Content)
	}

	// Verify the file was updated correctly.
	defPath := filepath.Join(cron.CronsDir(dataDir), "mycron.json")
	data, err := os.ReadFile(defPath)
	if err != nil {
		t.Fatalf("reading updated file: %v", err)
	}
	var updated cron.PromptCronDef
	if err := json.Unmarshal(data, &updated); err != nil {
		t.Fatalf("parsing updated file: %v", err)
	}

	if updated.Schedule != "0 9 * * *" {
		t.Errorf("schedule changed to %q, should remain unchanged", updated.Schedule)
	}
	if !updated.Enabled {
		t.Error("enabled should be true after update")
	}
	if updated.Prompt != "updated prompt" {
		t.Errorf("prompt = %q, want %q", updated.Prompt, "updated prompt")
	}
}

func TestDelete_NotFoundReturnsError(t *testing.T) {
	env, _ := testEnv(t)
	deps := Deps{ReloadFn: func() error { return nil }}
	deleteT := newDeleteTool(deps)

	args, _ := json.Marshal(map[string]string{"name": "nonexistent"})
	out, err := deleteT.Execute(context.Background(), args, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.IsError {
		t.Error("expected error for nonexistent cron")
	}
}
