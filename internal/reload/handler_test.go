package reload

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/flemzord/sclaw/internal/core"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestHandler_HandleReload_FileNotFound(t *testing.T) {
	logger := testLogger()
	appCtx := core.NewAppContext(logger, "/tmp/data", "/tmp/ws")
	app := core.NewApp(appCtx)
	h := NewHandler(app, logger, "/tmp/data", "/tmp/ws")

	err := h.HandleReload(context.Background(), "/nonexistent/config.yaml")
	if err == nil {
		t.Error("expected error for missing config file")
	}
}

func TestHandler_HandleReload_InvalidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	// Write valid YAML but invalid config (no version).
	if err := os.WriteFile(path, []byte("modules: {}"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	logger := testLogger()
	appCtx := core.NewAppContext(logger, "/tmp/data", "/tmp/ws")
	app := core.NewApp(appCtx)
	h := NewHandler(app, logger, "/tmp/data", "/tmp/ws")

	err := h.HandleReload(context.Background(), path)
	if err == nil {
		t.Error("expected validation error")
	}
}

func TestHandler_HandleReload_ValidConfig_NoReloaders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.yaml")
	// Minimal valid config with no modules (will fail validation because
	// at least one module is required).
	content := "version: \"1\"\nmodules:\n  fake.mod: {}\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	// Validation will fail because "fake.mod" is not registered.
	// This tests the validation error path through HandleReload.
	logger := testLogger()
	appCtx := core.NewAppContext(logger, "/tmp/data", "/tmp/ws")
	app := core.NewApp(appCtx)
	h := NewHandler(app, logger, "/tmp/data", "/tmp/ws")

	err := h.HandleReload(context.Background(), path)
	if err == nil {
		t.Error("expected validation error for unknown module")
	}
}
