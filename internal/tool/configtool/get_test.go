package configtool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/flemzord/sclaw/internal/security"
	"github.com/flemzord/sclaw/internal/tool"
)

func TestGetTool_Basic(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sclaw.yaml")
	content := []byte("version: \"1\"\nmodules:\n  test.mod:\n    key: value\n")

	if err := os.WriteFile(cfgPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	gt := newGetTool(Deps{
		ConfigPath: cfgPath,
		Redactor:   security.NewRedactor(),
	})

	out, err := gt.Execute(context.Background(), nil, tool.ExecutionEnv{})
	if err != nil {
		t.Fatal(err)
	}
	if out.IsError {
		t.Fatalf("unexpected error output: %s", out.Content)
	}

	var result getOutput
	if err := json.Unmarshal([]byte(out.Content), &result); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if result.BaseHash == "" {
		t.Error("base_hash should not be empty")
	}

	configMap, ok := result.Config.(map[string]any)
	if !ok {
		t.Fatalf("expected config to be a map, got %T", result.Config)
	}
	if configMap["version"] != "1" {
		t.Errorf("expected version=1, got %v", configMap["version"])
	}
}

func TestGetTool_RedactsSecrets(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sclaw.yaml")
	content := []byte("version: \"1\"\nmodules:\n  test.mod:\n    api_key: sk-ant-superSecretKey12345678901234567890\n")

	if err := os.WriteFile(cfgPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	gt := newGetTool(Deps{
		ConfigPath: cfgPath,
		Redactor:   security.NewRedactor(),
	})

	out, err := gt.Execute(context.Background(), nil, tool.ExecutionEnv{})
	if err != nil {
		t.Fatal(err)
	}

	if out.IsError {
		t.Fatalf("unexpected error: %s", out.Content)
	}

	// The output should contain the redaction placeholder, not the actual key.
	if json.Valid([]byte(out.Content)) {
		var raw map[string]any
		if err := json.Unmarshal([]byte(out.Content), &raw); err != nil {
			t.Fatalf("failed to unmarshal output: %v", err)
		}
		// Secrets should be redacted by the Redactor.
		if cfgRaw, ok := raw["config"].(map[string]any); ok {
			if modules, ok := cfgRaw["modules"].(map[string]any); ok {
				if mod, ok := modules["test.mod"].(map[string]any); ok {
					apiKey, _ := mod["api_key"].(string)
					if apiKey == "sk-ant-superSecretKey12345678901234567890" {
						t.Error("api_key should have been redacted")
					}
				}
			}
		}
	}
}

func TestGetTool_MissingFile(t *testing.T) {
	gt := newGetTool(Deps{ConfigPath: "/nonexistent/path/config.yaml"})

	out, err := gt.Execute(context.Background(), nil, tool.ExecutionEnv{})
	if err != nil {
		t.Fatal(err)
	}

	if !out.IsError {
		t.Error("expected error output for missing file")
	}
}
