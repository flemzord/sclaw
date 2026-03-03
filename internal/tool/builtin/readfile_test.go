package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flemzord/sclaw/internal/tool"
)

func TestReadFileTool(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	rt := &readFileTool{}

	// Create a test file.
	content := "hello world"
	if err := os.WriteFile(filepath.Join(workspace, "test.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a subdirectory.
	if err := os.MkdirAll(filepath.Join(workspace, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	env := tool.ExecutionEnv{Workspace: workspace}

	t.Run("read existing file", func(t *testing.T) {
		t.Parallel()
		args, _ := json.Marshal(readFileArgs{Path: "test.txt"})
		out, err := rt.Execute(context.Background(), args, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.IsError {
			t.Fatalf("unexpected tool error: %s", out.Content)
		}
		if out.Content != content {
			t.Errorf("content = %q, want %q", out.Content, content)
		}
	})

	t.Run("read relative path", func(t *testing.T) {
		t.Parallel()
		args, _ := json.Marshal(readFileArgs{Path: "./test.txt"})
		out, err := rt.Execute(context.Background(), args, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.IsError {
			t.Fatalf("unexpected tool error: %s", out.Content)
		}
		if out.Content != content {
			t.Errorf("content = %q, want %q", out.Content, content)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		t.Parallel()
		args, _ := json.Marshal(readFileArgs{Path: "nonexistent.txt"})
		out, err := rt.Execute(context.Background(), args, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !out.IsError {
			t.Error("expected tool error for nonexistent file")
		}
	})

	t.Run("path traversal blocked", func(t *testing.T) {
		t.Parallel()
		args, _ := json.Marshal(readFileArgs{Path: "../../../etc/passwd"})
		out, err := rt.Execute(context.Background(), args, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !out.IsError {
			t.Error("expected tool error for path traversal")
		}
	})

	t.Run("directory rejected", func(t *testing.T) {
		t.Parallel()
		args, _ := json.Marshal(readFileArgs{Path: "subdir"})
		out, err := rt.Execute(context.Background(), args, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !out.IsError {
			t.Error("expected tool error for directory")
		}
		if !strings.Contains(out.Content, "directory") {
			t.Errorf("error should mention directory, got: %s", out.Content)
		}
	})

	t.Run("file too large", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		largePath := filepath.Join(ws, "large.bin")
		// Create a file slightly over 1 MiB.
		if err := os.WriteFile(largePath, make([]byte, maxFileSize+1), 0o644); err != nil {
			t.Fatal(err)
		}
		args, _ := json.Marshal(readFileArgs{Path: "large.bin"})
		out, err := rt.Execute(context.Background(), args, tool.ExecutionEnv{Workspace: ws})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !out.IsError {
			t.Error("expected tool error for large file")
		}
		if !strings.Contains(out.Content, "too large") {
			t.Errorf("error should mention size, got: %s", out.Content)
		}
	})
}

func TestReadFileTool_Interface(t *testing.T) {
	t.Parallel()
	var _ tool.Tool = (*readFileTool)(nil)

	rt := &readFileTool{}
	if rt.Name() != "read_file" {
		t.Errorf("Name() = %q, want %q", rt.Name(), "read_file")
	}
	if len(rt.Scopes()) == 0 {
		t.Error("Scopes() should return at least one scope")
	}
}
