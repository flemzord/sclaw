// Package builtin provides the built-in tools (exec, read_file, write_file)
// that ship with every sclaw agent.
package builtin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flemzord/sclaw/internal/security"
)

// ErrPathTraversal is returned when a path resolves outside the workspace.
var ErrPathTraversal = errors.New("path traversal outside workspace")

// SafePath validates and resolves a file path within the workspace boundary.
// Relative paths are joined to the workspace. The resolved path must remain
// under the workspace after symlink resolution, and must pass
// security.ValidatePath (blocks /proc, /sys, /dev).
func SafePath(workspace, path string) (string, error) {
	if workspace == "" {
		return "", fmt.Errorf("%w: workspace is empty", ErrPathTraversal)
	}
	if path == "" {
		return "", fmt.Errorf("%w: path is empty", ErrPathTraversal)
	}

	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Join(workspace, cleaned)
	}

	// Resolve the workspace to an absolute, symlink-resolved path.
	wsResolved, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", fmt.Errorf("resolving workspace: %w", err)
	}

	// Resolve the target path. If the file doesn't exist yet (write_file),
	// resolve the parent directory instead.
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		parent := filepath.Dir(cleaned)
		parentResolved, parentErr := filepath.EvalSymlinks(parent)
		if parentErr != nil {
			return "", fmt.Errorf("%w: cannot resolve %s", ErrPathTraversal, path)
		}
		resolved = filepath.Join(parentResolved, filepath.Base(cleaned))
	}

	// Check that the resolved path is under the workspace.
	// Use separator suffix to prevent /workspace-extra from matching /workspace.
	if !strings.HasPrefix(resolved, wsResolved+string(filepath.Separator)) && resolved != wsResolved {
		return "", fmt.Errorf("%w: %s resolves outside workspace", ErrPathTraversal, path)
	}

	// Delegate to security.ValidatePath for restricted system paths.
	if err := security.ValidatePath(resolved); err != nil {
		return "", fmt.Errorf("%w: %w", ErrPathTraversal, err)
	}

	return resolved, nil
}

// safePathForWrite is like SafePath but creates parent directories if needed.
// It resolves the parent best-effort and verifies workspace containment.
func safePathForWrite(workspace, path string) (string, error) {
	if workspace == "" {
		return "", fmt.Errorf("%w: workspace is empty", ErrPathTraversal)
	}
	if path == "" {
		return "", fmt.Errorf("%w: path is empty", ErrPathTraversal)
	}

	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Join(workspace, cleaned)
	}

	wsResolved, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", fmt.Errorf("resolving workspace: %w", err)
	}

	// For writes, create the parent directory first.
	parent := filepath.Dir(cleaned)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("creating parent directory: %w", err)
	}

	// Resolve the parent (now guaranteed to exist).
	parentResolved, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", fmt.Errorf("%w: cannot resolve parent of %s", ErrPathTraversal, path)
	}
	resolved := filepath.Join(parentResolved, filepath.Base(cleaned))

	if !strings.HasPrefix(resolved, wsResolved+string(filepath.Separator)) && resolved != wsResolved {
		return "", fmt.Errorf("%w: %s resolves outside workspace", ErrPathTraversal, path)
	}

	if err := security.ValidatePath(resolved); err != nil {
		return "", fmt.Errorf("%w: %w", ErrPathTraversal, err)
	}

	return resolved, nil
}
