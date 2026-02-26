package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNew(t *testing.T) {
	t.Parallel()

	ws := New("/tmp/test-workspace")
	if ws.Root != "/tmp/test-workspace" {
		t.Errorf("Root = %q, want %q", ws.Root, "/tmp/test-workspace")
	}
}

func TestWorkspace_PathHelpers(t *testing.T) {
	t.Parallel()

	ws := New("/workspace")

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"SoulPath", ws.SoulPath(), filepath.Join("/workspace", "SOUL.md")},
		{"SkillsDir", ws.SkillsDir(), filepath.Join("/workspace", "skills")},
		{"MemoryDir", ws.MemoryDir(), filepath.Join("/workspace", "memory")},
		{"SessionsDir", ws.SessionsDir(), filepath.Join("/workspace", "sessions")},
		{"DataDir", ws.DataDir(), filepath.Join("/workspace", "data")},
	}

	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

func TestWorkspace_EnsureStructure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ws := New(filepath.Join(root, "agent"))

	if err := ws.EnsureStructure(); err != nil {
		t.Fatalf("EnsureStructure() error: %v", err)
	}

	dirs := []string{
		ws.Root,
		ws.SkillsDir(),
		ws.MemoryDir(),
		ws.SessionsDir(),
		ws.DataDir(),
	}

	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("directory %q does not exist: %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%q is not a directory", dir)
		}
	}
}

func TestWorkspace_EnsureStructure_Idempotent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ws := New(filepath.Join(root, "agent"))

	// Call twice — second call should not error.
	if err := ws.EnsureStructure(); err != nil {
		t.Fatalf("first EnsureStructure() error: %v", err)
	}
	if err := ws.EnsureStructure(); err != nil {
		t.Fatalf("second EnsureStructure() error: %v", err)
	}
}
