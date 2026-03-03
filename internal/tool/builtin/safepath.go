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

// SafePathForRead tries workspace first, then dataDir, then the PathFilter
// (any mode: RO or RW). Returns the first resolved path that succeeds.
// If filter is nil the behaviour is identical to the existing two-step fallback.
func SafePathForRead(workspace, dataDir, path string, filter *security.PathFilter) (string, error) {
	// Try workspace first.
	resolved, err := SafePath(workspace, path)
	if err == nil {
		return resolved, nil
	}

	// Try dataDir second.
	if dataDir != "" {
		resolved, err = SafePath(dataDir, path)
		if err == nil {
			return resolved, nil
		}
	}

	// Try allowed dirs (PathFilter) last.
	if filter != nil {
		return resolveViaFilter(path, filter.CheckRead)
	}

	return "", fmt.Errorf("%w: %s", ErrPathTraversal, path)
}

// safePathForWriteFiltered tries workspace first, then the PathFilter
// (RW directories only). dataDir is never writable.
func safePathForWriteFiltered(workspace, path string, filter *security.PathFilter) (string, error) {
	// Try workspace first.
	resolved, err := safePathForWrite(workspace, path)
	if err == nil {
		return resolved, nil
	}

	// Try allowed dirs (PathFilter, RW only) second.
	if filter != nil {
		return resolveViaFilter(path, filter.CheckWrite)
	}

	return "", fmt.Errorf("%w: %s", ErrPathTraversal, path)
}

// resolveViaFilter resolves an absolute path through a PathFilter check function.
// The path must be absolute and pass security.ValidatePath.
func resolveViaFilter(path string, check func(string) error) (string, error) {
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("%w: allowed_dirs requires absolute path, got %q", ErrPathTraversal, path)
	}

	// Resolve symlinks. If the file doesn't exist, resolve parent.
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		parent := filepath.Dir(cleaned)
		parentResolved, parentErr := filepath.EvalSymlinks(parent)
		if parentErr != nil {
			return "", fmt.Errorf("%w: cannot resolve %s", ErrPathTraversal, path)
		}
		resolved = filepath.Join(parentResolved, filepath.Base(cleaned))
	}

	if err := security.ValidatePath(resolved); err != nil {
		return "", fmt.Errorf("%w: %w", ErrPathTraversal, err)
	}

	if err := check(resolved); err != nil {
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
