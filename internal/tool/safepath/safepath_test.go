package safepath

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/flemzord/sclaw/internal/security"
)

func TestResolve(t *testing.T) {
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
			got, err := Resolve(tt.workspace, tt.path)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Resolve() = %q, want error", got)
				}
				if !errors.Is(err, ErrPathTraversal) {
					t.Errorf("Resolve() error = %v, want ErrPathTraversal", err)
				}
				return
			}
			if err != nil {
				t.Errorf("Resolve() error = %v, want nil", err)
			}
		})
	}
}

func TestResolve_SymlinkEscape(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	outside := t.TempDir()

	// Create a symlink inside the workspace that points outside.
	symlink := filepath.Join(workspace, "escape")
	if err := os.Symlink(outside, symlink); err != nil {
		t.Skip("symlinks not supported")
	}

	_, err := Resolve(workspace, "escape/secret.txt")
	if err == nil {
		t.Error("Resolve() should reject symlinks pointing outside workspace")
	}
	if !errors.Is(err, ErrPathTraversal) {
		t.Errorf("Resolve() error = %v, want ErrPathTraversal", err)
	}
}

func TestForWriteWorkspace(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()

	t.Run("creates parent directories", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		got, err := ForWriteWorkspace(ws, "a/b/c/file.txt")
		if err != nil {
			t.Fatalf("ForWriteWorkspace() error = %v", err)
		}
		if got == "" {
			t.Fatal("ForWriteWorkspace() returned empty path")
		}
		// Parent directory should have been created.
		parent := filepath.Dir(got)
		if _, err := os.Stat(parent); err != nil {
			t.Errorf("parent directory was not created: %v", err)
		}
	})

	t.Run("rejects traversal", func(t *testing.T) {
		t.Parallel()
		_, err := ForWriteWorkspace(workspace, "../../../tmp/evil.txt")
		if err == nil {
			t.Error("ForWriteWorkspace() should reject path traversal")
		}
		if !errors.Is(err, ErrPathTraversal) {
			t.Errorf("ForWriteWorkspace() error = %v, want ErrPathTraversal", err)
		}
	})
}

func TestForRead(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	dataDir := t.TempDir()
	allowedRO := t.TempDir()
	allowedRW := t.TempDir()

	// Resolve symlinks for macOS /var → /private/var.
	wsResolved, _ := filepath.EvalSymlinks(workspace)
	ddResolved, _ := filepath.EvalSymlinks(dataDir)
	roResolved, _ := filepath.EvalSymlinks(allowedRO)
	rwResolved, _ := filepath.EvalSymlinks(allowedRW)

	// Create files in each directory.
	for dir, name := range map[string]string{
		workspace: "ws.txt",
		dataDir:   "data.txt",
		allowedRO: "ro.txt",
		allowedRW: "rw.txt",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("ok"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	filter := security.NewPathFilter(security.PathFilterConfig{
		AllowedDirs: []security.AllowedDir{
			{Path: allowedRO, Mode: security.PathAccessRO},
			{Path: allowedRW, Mode: security.PathAccessRW},
		},
	})

	t.Run("workspace has priority", func(t *testing.T) {
		t.Parallel()
		got, err := ForRead(workspace, dataDir, "ws.txt", filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if filepath.Dir(got) != wsResolved {
			t.Errorf("expected workspace path %s, got %s", wsResolved, got)
		}
	})

	t.Run("fallback to dataDir", func(t *testing.T) {
		t.Parallel()
		got, err := ForRead(workspace, dataDir, filepath.Join(dataDir, "data.txt"), filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if filepath.Dir(got) != ddResolved {
			t.Errorf("expected dataDir path %s, got %s", ddResolved, got)
		}
	})

	t.Run("fallback to filter RO", func(t *testing.T) {
		t.Parallel()
		got, err := ForRead(workspace, dataDir, filepath.Join(allowedRO, "ro.txt"), filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if filepath.Dir(got) != roResolved {
			t.Errorf("expected allowedRO path %s, got %s", roResolved, got)
		}
	})

	t.Run("fallback to filter RW", func(t *testing.T) {
		t.Parallel()
		got, err := ForRead(workspace, dataDir, filepath.Join(allowedRW, "rw.txt"), filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if filepath.Dir(got) != rwResolved {
			t.Errorf("expected allowedRW path %s, got %s", rwResolved, got)
		}
	})

	t.Run("nil filter behaves like before", func(t *testing.T) {
		t.Parallel()
		_, err := ForRead(workspace, dataDir, "/some/random/path", nil)
		if err == nil {
			t.Error("expected error with nil filter and path outside workspace/dataDir")
		}
	})

	t.Run("reject path outside everything", func(t *testing.T) {
		t.Parallel()
		_, err := ForRead(workspace, dataDir, "/nonexistent/path/file.txt", filter)
		if err == nil {
			t.Error("expected error for path outside all allowed locations")
		}
	})
}

func TestForWrite(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	allowedRO := t.TempDir()
	allowedRW := t.TempDir()

	rwResolved, _ := filepath.EvalSymlinks(allowedRW)

	filter := security.NewPathFilter(security.PathFilterConfig{
		AllowedDirs: []security.AllowedDir{
			{Path: allowedRO, Mode: security.PathAccessRO},
			{Path: allowedRW, Mode: security.PathAccessRW},
		},
	})

	t.Run("workspace has priority", func(t *testing.T) {
		t.Parallel()
		ws := t.TempDir()
		wsResolved, _ := filepath.EvalSymlinks(ws)
		got, err := ForWrite(ws, "newfile.txt", filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if filepath.Dir(got) != wsResolved {
			t.Errorf("expected workspace path %s, got %s", wsResolved, got)
		}
	})

	t.Run("RW dir allowed", func(t *testing.T) {
		t.Parallel()
		got, err := ForWrite(workspace, filepath.Join(allowedRW, "output.txt"), filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if filepath.Dir(got) != rwResolved {
			t.Errorf("expected allowedRW path %s, got %s", rwResolved, got)
		}
	})

	t.Run("RO dir rejected for write", func(t *testing.T) {
		t.Parallel()
		_, err := ForWrite(workspace, filepath.Join(allowedRO, "output.txt"), filter)
		if err == nil {
			t.Error("expected error: writing to RO directory should be rejected")
		}
	})

	t.Run("nil filter same as before", func(t *testing.T) {
		t.Parallel()
		_, err := ForWrite(workspace, "/random/path/file.txt", nil)
		if err == nil {
			t.Error("expected error with nil filter and path outside workspace")
		}
	})
}
