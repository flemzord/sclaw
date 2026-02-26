// Package workspace manages the agent workspace directory structure,
// SOUL.md personality loading, and SKILL.md skill activation.
package workspace

import (
	"os"
	"path/filepath"
)

// Workspace represents the agent workspace directory structure.
// It provides path helpers and ensures the required subdirectories exist.
type Workspace struct {
	Root string
}

// New creates a new Workspace rooted at the given directory.
func New(root string) *Workspace {
	return &Workspace{Root: root}
}

// EnsureStructure creates the workspace directory tree if it does not exist.
// Idempotent — safe to call multiple times.
func (w *Workspace) EnsureStructure() error {
	dirs := []string{
		w.Root,
		w.SkillsDir(),
		w.MemoryDir(),
		w.SessionsDir(),
		w.DataDir(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// SoulPath returns the path to the SOUL.md personality file.
func (w *Workspace) SoulPath() string {
	return filepath.Join(w.Root, "SOUL.md")
}

// SkillsDir returns the path to the skills directory.
func (w *Workspace) SkillsDir() string {
	return filepath.Join(w.Root, "skills")
}

// MemoryDir returns the path to the memory directory.
func (w *Workspace) MemoryDir() string {
	return filepath.Join(w.Root, "memory")
}

// SessionsDir returns the path to the sessions directory.
func (w *Workspace) SessionsDir() string {
	return filepath.Join(w.Root, "sessions")
}

// DataDir returns the path to the data directory.
func (w *Workspace) DataDir() string {
	return filepath.Join(w.Root, "data")
}
