// Package builtin provides the built-in tools (exec, read_file, write_file)
// that ship with every sclaw agent.
package builtin

import (
	"github.com/flemzord/sclaw/internal/security"
	"github.com/flemzord/sclaw/internal/tool/safepath"
)

// ErrPathTraversal is returned when a path resolves outside the workspace.
var ErrPathTraversal = safepath.ErrPathTraversal

// SafePath validates and resolves a file path within the workspace boundary.
func SafePath(workspace, path string) (string, error) {
	return safepath.Resolve(workspace, path)
}

// SafePathForRead tries workspace first, then dataDir, then the PathFilter.
func SafePathForRead(workspace, dataDir, path string, filter *security.PathFilter) (string, error) {
	return safepath.ForRead(workspace, dataDir, path, filter)
}

// safePathForWriteFiltered tries workspace first, then the PathFilter (RW only).
func safePathForWriteFiltered(workspace, path string, filter *security.PathFilter) (string, error) {
	return safepath.ForWrite(workspace, path, filter)
}

// safePathForWrite is like SafePath but creates parent directories if needed.
func safePathForWrite(workspace, path string) (string, error) {
	return safepath.ForWriteWorkspace(workspace, path)
}
