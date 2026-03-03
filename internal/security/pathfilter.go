package security

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrPathBlocked is returned when a path is denied by the filter.
var ErrPathBlocked = errors.New("path blocked by filter")

// PathAccessMode declares the access level for an allowed directory.
type PathAccessMode string

// PathAccessMode values.
const (
	PathAccessRO PathAccessMode = "ro"
	PathAccessRW PathAccessMode = "rw"
)

// AllowedDir pairs a directory path with its access mode.
type AllowedDir struct {
	Path string
	Mode PathAccessMode
}

// PathFilterConfig holds the configuration for path filtering.
type PathFilterConfig struct {
	AllowedDirs []AllowedDir
}

// PathFilter implements directory-based path access control.
// It validates that resolved absolute paths fall within one of the
// configured allowed directories with the appropriate access mode.
type PathFilter struct {
	dirs []AllowedDir
}

// NewPathFilter creates a PathFilter from the given config.
// Entries with relative paths are silently dropped. Empty or
// unrecognised modes default to "ro". Directory paths are resolved
// through EvalSymlinks for consistency with SafePath's symlink resolution.
func NewPathFilter(cfg PathFilterConfig) *PathFilter {
	dirs := make([]AllowedDir, 0, len(cfg.AllowedDirs))
	for _, d := range cfg.AllowedDirs {
		cleaned := filepath.Clean(d.Path)
		if !filepath.IsAbs(cleaned) {
			continue
		}
		// Resolve symlinks so the stored path matches what EvalSymlinks
		// produces when checking file paths (e.g. /var → /private/var on macOS).
		if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
			cleaned = resolved
		}
		mode := normalizeMode(d.Mode)
		dirs = append(dirs, AllowedDir{Path: cleaned, Mode: mode})
	}
	return &PathFilter{dirs: dirs}
}

// CheckRead validates that resolvedPath is within any allowed directory
// (either RO or RW). Returns nil if allowed, ErrPathBlocked if denied.
func (f *PathFilter) CheckRead(resolvedPath string) error {
	for _, d := range f.dirs {
		if isUnder(resolvedPath, d.Path) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s (not in any allowed directory)", ErrPathBlocked, resolvedPath)
}

// CheckWrite validates that resolvedPath is within an RW allowed directory.
// Returns nil if allowed, ErrPathBlocked if denied.
func (f *PathFilter) CheckWrite(resolvedPath string) error {
	for _, d := range f.dirs {
		if d.Mode == PathAccessRW && isUnder(resolvedPath, d.Path) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s (not in any read-write allowed directory)", ErrPathBlocked, resolvedPath)
}

// Dirs returns a copy of the configured allowed directories.
func (f *PathFilter) Dirs() []AllowedDir {
	out := make([]AllowedDir, len(f.dirs))
	copy(out, f.dirs)
	return out
}

// isUnder checks whether resolved is equal to dir or is a child of dir.
// Uses separator suffix to prevent /workspace-extra from matching /workspace.
func isUnder(resolved, dir string) bool {
	if resolved == dir {
		return true
	}
	return strings.HasPrefix(resolved, dir+string(filepath.Separator))
}

// normalizeMode returns a valid PathAccessMode, defaulting to RO.
func normalizeMode(m PathAccessMode) PathAccessMode {
	switch m {
	case PathAccessRO, PathAccessRW:
		return m
	default:
		return PathAccessRO
	}
}
