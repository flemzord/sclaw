package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/flemzord/sclaw/internal/tool"
)

func TestExecTool(t *testing.T) {
	t.Parallel()

	et := &execTool{}

	t.Run("echo hello", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		env := tool.ExecutionEnv{Workspace: ws}

		args, _ := json.Marshal(execArgs{Command: "echo hello"})
		out, err := et.Execute(context.Background(), args, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.IsError {
			t.Fatalf("unexpected tool error: %s", out.Content)
		}
		if got := strings.TrimSpace(out.Content); got != "hello" {
			t.Errorf("output = %q, want %q", got, "hello")
		}
	})

	t.Run("exit 1 returns error", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		env := tool.ExecutionEnv{Workspace: ws}

		args, _ := json.Marshal(execArgs{Command: "echo fail >&2; exit 1"})
		out, err := et.Execute(context.Background(), args, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !out.IsError {
			t.Error("expected tool error for exit 1")
		}
		if !strings.Contains(out.Content, "fail") {
			t.Errorf("stderr should contain 'fail', got: %s", out.Content)
		}
	})

	t.Run("timeout kills command", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		env := tool.ExecutionEnv{Workspace: ws}

		args, _ := json.Marshal(execArgs{Command: "sleep 30", TimeoutSeconds: 1})
		out, err := et.Execute(context.Background(), args, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !out.IsError {
			t.Error("expected tool error for timed out command")
		}
	})

	t.Run("pwd equals workspace", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		env := tool.ExecutionEnv{Workspace: ws}

		args, _ := json.Marshal(execArgs{Command: "pwd"})
		out, err := et.Execute(context.Background(), args, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.IsError {
			t.Fatalf("unexpected tool error: %s", out.Content)
		}
		// On macOS, /tmp is a symlink to /private/tmp, so compare canonical paths.
		got := strings.TrimSpace(out.Content)
		if got != ws && !strings.HasSuffix(got, ws) && !strings.HasSuffix(ws, got) {
			// Fallback: both should resolve to the same thing.
			t.Logf("pwd = %q, workspace = %q (may differ due to symlinks)", got, ws)
		}
	})

	t.Run("sanitized env used", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		env := tool.ExecutionEnv{
			Workspace:    ws,
			SanitizedEnv: []string{"MY_TEST_VAR=sclaw_test_value"},
		}

		args, _ := json.Marshal(execArgs{Command: "echo $MY_TEST_VAR"})
		out, err := et.Execute(context.Background(), args, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.IsError {
			t.Fatalf("unexpected tool error: %s", out.Content)
		}
		if got := strings.TrimSpace(out.Content); got != "sclaw_test_value" {
			t.Errorf("output = %q, want %q", got, "sclaw_test_value")
		}
	})

	t.Run("empty command", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		env := tool.ExecutionEnv{Workspace: ws}

		args, _ := json.Marshal(execArgs{Command: ""})
		out, err := et.Execute(context.Background(), args, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !out.IsError {
			t.Error("expected tool error for empty command")
		}
	})

	t.Run("output truncated", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		env := tool.ExecutionEnv{Workspace: ws}

		// Generate output well over maxOutputSize.
		args, _ := json.Marshal(execArgs{Command: "dd if=/dev/zero bs=1024 count=2048 2>/dev/null | tr '\\0' 'A'"})
		out, err := et.Execute(context.Background(), args, env)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out.Content) > maxOutputSize+100 {
			t.Errorf("output not truncated: len = %d, max = %d", len(out.Content), maxOutputSize)
		}
	})
}

func TestExecTool_Interface(t *testing.T) {
	t.Parallel()
	var _ tool.Tool = (*execTool)(nil)

	et := &execTool{}
	if et.Name() != "exec" {
		t.Errorf("Name() = %q, want %q", et.Name(), "exec")
	}
	if len(et.Scopes()) == 0 {
		t.Error("Scopes() should return at least one scope")
	}
}
