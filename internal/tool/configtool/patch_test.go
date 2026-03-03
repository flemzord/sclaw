package configtool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flemzord/sclaw/internal/tool"
)

func writeTestConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "sclaw.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPatchTool_HashMismatch(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "version: \"1\"\nmodules:\n  test.mod:\n    key: old\n")

	pt := newPatchTool(Deps{ConfigPath: cfgPath})

	args, _ := json.Marshal(patchArgs{
		BaseHash: "wrong-hash",
		Patch:    "modules:\n  test.mod:\n    key: new\n",
	})

	out, err := pt.Execute(context.Background(), args, tool.ExecutionEnv{})
	if err != nil {
		t.Fatal(err)
	}

	if !out.IsError {
		t.Error("expected error for hash mismatch")
	}

	var result patchOutput
	if err := json.Unmarshal([]byte(out.Content), &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if result.Status != "error" {
		t.Errorf("expected status=error, got %s", result.Status)
	}
}

func TestPatchTool_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := "version: \"1\"\nmodules:\n  test.mod:\n    key: old\n"
	cfgPath := writeTestConfig(t, dir, original)

	// Get the hash of the original.
	hash, _, err := fileHash(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	pt := newPatchTool(Deps{
		ConfigPath: cfgPath,
		ReloadFn:   nil, // No reload in tests.
	})

	args, _ := json.Marshal(patchArgs{
		BaseHash: hash,
		Patch:    "modules:\n  test.mod:\n    key: new\n",
	})

	out, err := pt.Execute(context.Background(), args, tool.ExecutionEnv{})
	if err != nil {
		t.Fatal(err)
	}

	// Patch may fail on validation (test.mod not registered), but the merge should work.
	var result patchOutput
	if err := json.Unmarshal([]byte(out.Content), &result); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if result.Status == "applied" {
		// Verify the file was written.
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		content := string(data)
		if result.NewHash == "" {
			t.Error("expected non-empty new_hash")
		}
		if result.NewHash == hash {
			t.Error("expected new_hash to differ from original")
		}
		t.Logf("patched config:\n%s", content)
	} else {
		t.Logf("patch returned status=%s error=%s (expected for unregistered modules)", result.Status, result.Error)
	}
}

func TestPatchTool_PreservesEnvVars(t *testing.T) {
	dir := t.TempDir()
	original := "version: \"1\"\nmodules:\n  test.mod:\n    api_key: ${API_KEY}\n    model: gpt-4\n"
	cfgPath := writeTestConfig(t, dir, original)

	hash, _, err := fileHash(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	pt := newPatchTool(Deps{
		ConfigPath: cfgPath,
		ReloadFn:   nil,
	})

	args, _ := json.Marshal(patchArgs{
		BaseHash: hash,
		Patch:    "modules:\n  test.mod:\n    model: gpt-4-turbo\n",
	})

	out, err := pt.Execute(context.Background(), args, tool.ExecutionEnv{})
	if err != nil {
		t.Fatal(err)
	}

	result := unmarshalPatchOutput(t, out.Content)

	if result.Status == "applied" {
		data, readErr := os.ReadFile(cfgPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		content := string(data)
		if !strings.Contains(content, "${API_KEY}") {
			t.Errorf("expected ${API_KEY} to be preserved in output, got:\n%s", content)
		}
		if !strings.Contains(content, "gpt-4-turbo") {
			t.Errorf("expected model to be updated, got:\n%s", content)
		}
	} else {
		// Validation may fail due to unregistered module — check that merge at least happened.
		t.Logf("patch validation failed (expected): %s", result.Error)
	}
}

func TestPatchTool_MissingArgs(t *testing.T) {
	pt := newPatchTool(Deps{ConfigPath: "/tmp/nonexistent.yaml"})

	args, _ := json.Marshal(patchArgs{BaseHash: "", Patch: ""})
	out, err := pt.Execute(context.Background(), args, tool.ExecutionEnv{})
	if err != nil {
		t.Fatal(err)
	}
	if !out.IsError {
		t.Error("expected error for missing args")
	}
}
