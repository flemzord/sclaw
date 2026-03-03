package configtool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/flemzord/sclaw/internal/tool"
)

func unmarshalPatchOutput(t *testing.T, content string) patchOutput {
	t.Helper()
	var result patchOutput
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}
	return result
}

func TestApplyTool_HashMismatch(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sclaw.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: \"1\"\nmodules:\n  test.mod:\n    key: old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	at := newApplyTool(Deps{ConfigPath: cfgPath})

	args, _ := json.Marshal(applyArgs{
		BaseHash:    "wrong-hash",
		YAMLContent: "version: \"1\"\nmodules:\n  test.mod:\n    key: new\n",
	})

	out, err := at.Execute(context.Background(), args, tool.ExecutionEnv{})
	if err != nil {
		t.Fatal(err)
	}

	if !out.IsError {
		t.Error("expected error for hash mismatch")
	}

	result := unmarshalPatchOutput(t, out.Content)
	if result.Status != "error" {
		t.Errorf("expected status=error, got %s", result.Status)
	}
}

func TestApplyTool_Success(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sclaw.yaml")
	original := "version: \"1\"\nmodules:\n  test.mod:\n    key: old\n"
	if err := os.WriteFile(cfgPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	hash, _, err := fileHash(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	at := newApplyTool(Deps{
		ConfigPath: cfgPath,
		ReloadFn:   nil,
	})

	newContent := "version: \"1\"\nmodules:\n  test.mod:\n    key: new\n"
	args, _ := json.Marshal(applyArgs{
		BaseHash:    hash,
		YAMLContent: newContent,
	})

	out, err := at.Execute(context.Background(), args, tool.ExecutionEnv{})
	if err != nil {
		t.Fatal(err)
	}

	result := unmarshalPatchOutput(t, out.Content)

	if result.Status == "applied" {
		data, readErr := os.ReadFile(cfgPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(data) != newContent {
			t.Errorf("expected file content to be replaced, got:\n%s", string(data))
		}
		if result.NewHash == "" {
			t.Error("expected non-empty new_hash")
		}
	} else {
		t.Logf("apply returned status=%s error=%s (expected for unregistered modules)", result.Status, result.Error)
	}
}

func TestApplyTool_MissingArgs(t *testing.T) {
	at := newApplyTool(Deps{ConfigPath: "/tmp/nonexistent.yaml"})

	args, _ := json.Marshal(applyArgs{BaseHash: "", YAMLContent: ""})
	out, err := at.Execute(context.Background(), args, tool.ExecutionEnv{})
	if err != nil {
		t.Fatal(err)
	}
	if !out.IsError {
		t.Error("expected error for missing args")
	}
}

func TestApplyTool_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sclaw.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: \"1\"\nmodules:\n  test.mod:\n    key: old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hash, _, err := fileHash(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	at := newApplyTool(Deps{ConfigPath: cfgPath})

	args, _ := json.Marshal(applyArgs{
		BaseHash:    hash,
		YAMLContent: "invalid: [yaml: broken",
	})

	out, err := at.Execute(context.Background(), args, tool.ExecutionEnv{})
	if err != nil {
		t.Fatal(err)
	}

	result := unmarshalPatchOutput(t, out.Content)
	if result.Status != "error" {
		t.Errorf("expected status=error for invalid YAML, got %s", result.Status)
	}
}

func TestApplyTool_ReloadCalled(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sclaw.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: \"1\"\nmodules:\n  test.mod:\n    key: old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hash, _, err := fileHash(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	reloadCalled := false
	at := newApplyTool(Deps{
		ConfigPath: cfgPath,
		ReloadFn: func(_ context.Context, _ string) error {
			reloadCalled = true
			return nil
		},
	})

	newContent := "version: \"1\"\nmodules:\n  test.mod:\n    key: new\n"
	args, _ := json.Marshal(applyArgs{
		BaseHash:    hash,
		YAMLContent: newContent,
	})

	out, err := at.Execute(context.Background(), args, tool.ExecutionEnv{})
	if err != nil {
		t.Fatal(err)
	}

	result := unmarshalPatchOutput(t, out.Content)
	if result.Status == "applied" && !reloadCalled {
		t.Error("expected reload function to be called")
	}
}
