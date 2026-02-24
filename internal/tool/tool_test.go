package tool

import (
	"testing"
)

func TestScope_Constants(t *testing.T) {
	t.Parallel()

	scopes := []Scope{ScopeReadOnly, ScopeReadWrite, ScopeExec, ScopeNetwork}
	want := []string{"read_only", "read_write", "exec", "network"}

	for i, s := range scopes {
		if string(s) != want[i] {
			t.Errorf("scope %d: got %q, want %q", i, s, want[i])
		}
	}
}

func TestOutput_Zero(t *testing.T) {
	t.Parallel()

	var out Output
	if out.Content != "" {
		t.Errorf("zero Content should be empty, got %q", out.Content)
	}
	if out.IsError {
		t.Error("zero IsError should be false")
	}
}

func TestExecutionEnv_Zero(t *testing.T) {
	t.Parallel()

	var env ExecutionEnv
	if env.Workspace != "" || env.DataDir != "" {
		t.Error("zero ExecutionEnv fields should be empty")
	}
}
