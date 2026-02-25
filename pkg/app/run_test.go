package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConfigPath_XDGConfigHome(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "sclaw")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfgPath := filepath.Join(cfgDir, "sclaw.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: \"1\""), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	t.Setenv("XDG_CONFIG_HOME", dir)

	got, err := ResolveConfigPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != cfgPath {
		t.Errorf("got %q, want %q", got, cfgPath)
	}
}

func TestResolveConfigPath_NotFound(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/nonexistent/path")

	// Also ensure there's no sclaw.yaml in the current directory.
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	_, err := ResolveConfigPath()
	if err == nil {
		t.Error("expected error when no config file found")
	}
}

func TestDefaultDataDir_XDGDataHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/custom/data")
	got := DefaultDataDir()
	want := "/custom/data/sclaw"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDefaultDataDir_Fallback(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	_ = os.Unsetenv("XDG_DATA_HOME")

	got := DefaultDataDir()
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "sclaw", "data")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDefaultWorkspace(t *testing.T) {
	got := DefaultWorkspace()
	cwd, _ := os.Getwd()
	if got != cwd {
		t.Errorf("got %q, want %q", got, cwd)
	}
}

func TestRun_InvalidConfigPath(t *testing.T) {
	err := Run(RunParams{ConfigPath: "/nonexistent/config.yaml"})
	if err == nil {
		t.Error("expected error for invalid config path")
	}
}

func TestRun_InvalidConfigContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("not: valid: yaml: ["), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := Run(RunParams{ConfigPath: path})
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestRun_ValidationFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "noversion.yaml")
	if err := os.WriteFile(path, []byte("modules:\n  foo: {}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := Run(RunParams{ConfigPath: path})
	if err == nil {
		t.Error("expected validation error")
	}
}
