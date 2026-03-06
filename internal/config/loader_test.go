package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `version: "1"
modules:
  test.mod:
    key: value
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Version != "1" {
		t.Errorf("version = %q, want %q", cfg.Version, "1")
	}
	if len(cfg.Modules) != 1 {
		t.Errorf("modules count = %d, want 1", len(cfg.Modules))
	}
	if _, ok := cfg.Modules["test.mod"]; !ok {
		t.Error("module test.mod not found")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !strings.Contains(err.Error(), "reading") {
		t.Errorf("error should mention reading: %v", err)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte(":\n  :\n[broken"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "parsing") {
		t.Errorf("error should mention parsing: %v", err)
	}
}

func TestExpandEnv_SimpleVar(t *testing.T) {
	t.Setenv("TEST_TOKEN", "secret123")
	result, err := expandEnv([]byte("token: ${TEST_TOKEN}"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != "token: secret123" {
		t.Errorf("got %q, want %q", string(result), "token: secret123")
	}
}

func TestExpandEnv_WithDefault_EnvSet(t *testing.T) {
	t.Setenv("MY_VAR", "from_env")
	result, err := expandEnv([]byte("val: ${MY_VAR:-fallback}"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != "val: from_env" {
		t.Errorf("got %q, want %q", string(result), "val: from_env")
	}
}

func TestExpandEnv_WithDefault_EnvUnset(t *testing.T) {
	result, err := expandEnv([]byte("val: ${UNSET_VAR_12345:-fallback}"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != "val: fallback" {
		t.Errorf("got %q, want %q", string(result), "val: fallback")
	}
}

func TestExpandEnv_EmptyDefault(t *testing.T) {
	result, err := expandEnv([]byte("val: ${UNSET_VAR_12345:-}"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != "val: " {
		t.Errorf("got %q, want %q", string(result), "val: ")
	}
}

func TestExpandEnv_UnresolvedError(t *testing.T) {
	_, err := expandEnv([]byte("token: ${MISSING_VAR_99999}"))
	if err == nil {
		t.Fatal("expected error for unresolved variable")
	}
	if !strings.Contains(err.Error(), "MISSING_VAR_99999") {
		t.Errorf("error should mention variable name: %v", err)
	}
}

func TestExpandEnv_MultipleVars(t *testing.T) {
	t.Setenv("HOST_TEST", "localhost")
	t.Setenv("PORT_TEST", "8080")
	result, err := expandEnv([]byte("url: ${HOST_TEST}:${PORT_TEST}"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != "url: localhost:8080" {
		t.Errorf("got %q, want %q", string(result), "url: localhost:8080")
	}
}

func TestExpandEnv_NoVars(t *testing.T) {
	input := "plain: value\nno_vars: here"
	result, err := expandEnv([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result) != input {
		t.Errorf("got %q, want %q", string(result), input)
	}
}

func TestExpandEnv_MultipleErrors(t *testing.T) {
	_, err := expandEnv([]byte("a: ${MISS_A_99}\nb: ${MISS_B_99}"))
	if err == nil {
		t.Fatal("expected error for unresolved variables")
	}
	if !strings.Contains(err.Error(), "MISS_A_99") {
		t.Errorf("error should mention MISS_A_99: %v", err)
	}
	if !strings.Contains(err.Error(), "MISS_B_99") {
		t.Errorf("error should mention MISS_B_99: %v", err)
	}
}

func TestLoad_WithOnePasswordExpansion(t *testing.T) {
	stubOP(t, func(ref, _ string) (string, error) {
		if ref == "op://Private/sclaw/test-key" {
			return "op_resolved_value", nil
		}
		return "", fmt.Errorf("unexpected ref: %s", ref)
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `version: "1"
modules:
  test.mod:
    key: "op://Private/sclaw/test-key"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	node := cfg.Modules["test.mod"]
	var parsed struct {
		Key string `yaml:"key"`
	}
	if err := node.Decode(&parsed); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if parsed.Key != "op_resolved_value" {
		t.Errorf("key = %q, want %q", parsed.Key, "op_resolved_value")
	}
}

func TestLoad_WithEnvExpansion(t *testing.T) {
	t.Setenv("SCLAW_TEST_KEY", "expanded_value")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `version: "1"
modules:
  test.mod:
    key: "${SCLAW_TEST_KEY}"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	node := cfg.Modules["test.mod"]
	var parsed struct {
		Key string `yaml:"key"`
	}
	if err := node.Decode(&parsed); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if parsed.Key != "expanded_value" {
		t.Errorf("key = %q, want %q", parsed.Key, "expanded_value")
	}
}
