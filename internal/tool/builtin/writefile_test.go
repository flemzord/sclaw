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

func TestWriteFileTool(t *testing.T) {
	t.Parallel()

	wt := &writeFileTool{}

	t.Run("write new file", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		env := tool.ExecutionEnv{Workspace: ws}

		args, _ := json.Marshal(writeFileArgs{Path: "out.txt", Content: "hello"})
		out, err := wt.Execute(context.Background(), args, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.IsError {
			t.Fatalf("unexpected tool error: %s", out.Content)
		}

		// Verify file contents.
		data, err := os.ReadFile(filepath.Join(ws, "out.txt"))
		if err != nil {
			t.Fatalf("reading written file: %v", err)
		}
		if string(data) != "hello" {
			t.Errorf("content = %q, want %q", string(data), "hello")
		}
	})

	t.Run("creates subdirectories", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		env := tool.ExecutionEnv{Workspace: ws}

		args, _ := json.Marshal(writeFileArgs{Path: "a/b/c/deep.txt", Content: "nested"})
		out, err := wt.Execute(context.Background(), args, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.IsError {
			t.Fatalf("unexpected tool error: %s", out.Content)
		}

		data, err := os.ReadFile(filepath.Join(ws, "a", "b", "c", "deep.txt"))
		if err != nil {
			t.Fatalf("reading written file: %v", err)
		}
		if string(data) != "nested" {
			t.Errorf("content = %q, want %q", string(data), "nested")
		}
	})

	t.Run("path traversal blocked", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		env := tool.ExecutionEnv{Workspace: ws}

		args, _ := json.Marshal(writeFileArgs{Path: "../../../tmp/evil.txt", Content: "bad"})
		out, err := wt.Execute(context.Background(), args, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !out.IsError {
			t.Error("expected tool error for path traversal")
		}
	})

	t.Run("content too large", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		env := tool.ExecutionEnv{Workspace: ws}

		large := strings.Repeat("x", maxWriteSize+1)
		args, _ := json.Marshal(writeFileArgs{Path: "big.txt", Content: large})
		out, err := wt.Execute(context.Background(), args, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !out.IsError {
			t.Error("expected tool error for oversized content")
		}
		if !strings.Contains(out.Content, "too large") {
			t.Errorf("error should mention size, got: %s", out.Content)
		}
	})

	t.Run("read back written content", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		env := tool.ExecutionEnv{Workspace: ws}

		content := "verify round-trip"
		writeArgs, _ := json.Marshal(writeFileArgs{Path: "verify.txt", Content: content})
		out, err := wt.Execute(context.Background(), writeArgs, env)
		if err != nil {
			t.Fatalf("write error: %v", err)
		}
		if out.IsError {
			t.Fatalf("write tool error: %s", out.Content)
		}

		// Read back with read_file tool.
		rt := &readFileTool{}
		readArgs, _ := json.Marshal(readFileArgs{Path: "verify.txt"})
		out, err = rt.Execute(context.Background(), readArgs, env)
		if err != nil {
			t.Fatalf("read error: %v", err)
		}
		if out.IsError {
			t.Fatalf("read tool error: %s", out.Content)
		}
		if out.Content != content {
			t.Errorf("round-trip content = %q, want %q", out.Content, content)
		}
	})
}

func TestWriteFileTool_Interface(t *testing.T) {
	t.Parallel()
	var _ tool.Tool = (*writeFileTool)(nil)

	wt := &writeFileTool{}
	if wt.Name() != "write_file" {
		t.Errorf("Name() = %q, want %q", wt.Name(), "write_file")
	}
	if len(wt.Scopes()) == 0 {
		t.Error("Scopes() should return at least one scope")
	}
}
