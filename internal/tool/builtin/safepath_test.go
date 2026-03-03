package builtin

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSafePath(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	// Create a file inside the workspace for testing.
	testFile := filepath.Join(workspace, "hello.txt")
	if err := os.WriteFile(testFile, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a subdirectory.
	subDir := filepath.Join(workspace, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		workspace string
		path      string
		wantErr   bool
	}{
		{
			name:      "relative path OK",
			workspace: workspace,
			path:      "hello.txt",
		},
		{
			name:      "absolute path inside workspace OK",
			workspace: workspace,
			path:      testFile,
		},
		{
			name:      "dotdot inside workspace OK",
			workspace: workspace,
			path:      "sub/../hello.txt",
		},
		{
			name:      "dotdot escapes workspace",
			workspace: workspace,
			path:      "../../../etc/passwd",
			wantErr:   true,
		},
		{
			name:      "absolute path outside workspace",
			workspace: workspace,
			path:      "/etc/passwd",
			wantErr:   true,
		},
		{
			name:      "workspace is empty",
			workspace: "",
			path:      "file.txt",
			wantErr:   true,
		},
		{
			name:      "path is empty",
			workspace: workspace,
			path:      "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := SafePath(tt.workspace, tt.path)
			if tt.wantErr {
				if err == nil {
					t.Errorf("SafePath() = %q, want error", got)
				}
				if !errors.Is(err, ErrPathTraversal) {
					t.Errorf("SafePath() error = %v, want ErrPathTraversal", err)
				}
				return
			}
			if err != nil {
				t.Errorf("SafePath() error = %v, want nil", err)
			}
		})
	}
}

func TestSafePath_SymlinkEscape(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	outside := t.TempDir()

	// Create a symlink inside the workspace that points outside.
	symlink := filepath.Join(workspace, "escape")
	if err := os.Symlink(outside, symlink); err != nil {
		t.Skip("symlinks not supported")
	}

	_, err := SafePath(workspace, "escape/secret.txt")
	if err == nil {
		t.Error("SafePath() should reject symlinks pointing outside workspace")
	}
	if !errors.Is(err, ErrPathTraversal) {
		t.Errorf("SafePath() error = %v, want ErrPathTraversal", err)
	}
}

func TestSafePathForWrite(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	t.Run("creates parent directories", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		got, err := safePathForWrite(ws, "a/b/c/file.txt")
		if err != nil {
			t.Fatalf("safePathForWrite() error = %v", err)
		}
		if got == "" {
			t.Fatal("safePathForWrite() returned empty path")
		}
		// Parent directory should have been created.
		parent := filepath.Dir(got)
		if _, err := os.Stat(parent); err != nil {
			t.Errorf("parent directory was not created: %v", err)
		}
	})

	t.Run("rejects traversal", func(t *testing.T) {
		t.Parallel()
		_, err := safePathForWrite(workspace, "../../../tmp/evil.txt")
		if err == nil {
			t.Error("safePathForWrite() should reject path traversal")
		}
		if !errors.Is(err, ErrPathTraversal) {
			t.Errorf("safePathForWrite() error = %v, want ErrPathTraversal", err)
		}
	})
}
