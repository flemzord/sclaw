package bootstrap

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Bootstrapper detects plugin-list changes and rebuilds the binary.
type Bootstrapper struct {
	currentHash string
	binaryPath  string
	xsclawPath  string // absolute path to the xsclaw binary
}

// NewBootstrapper creates a Bootstrapper with the given build hash
// (injected at compile time via ldflags). Returns an error if the
// current executable path cannot be determined.
func NewBootstrapper(currentHash string) (*Bootstrapper, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("determining executable path: %w", err)
	}

	// Resolve xsclaw as a sibling of the running binary to avoid PATH hijack.
	xsclaw := filepath.Join(filepath.Dir(exe), "xsclaw")

	return &Bootstrapper{
		currentHash: currentHash,
		binaryPath:  exe,
		xsclawPath:  xsclaw,
	}, nil
}

// NeedsRebuild returns true when the desired plugin set differs from the
// set that was compiled into the running binary.
func (b *Bootstrapper) NeedsRebuild(desiredPlugins []string) bool {
	if b.currentHash == "" {
		return false
	}
	return BuildHash(desiredPlugins) != b.currentHash
}

// Rebuild invokes xsclaw build to produce a new binary containing the given
// plugins. It returns the path to the newly built binary.
func (b *Bootstrapper) Rebuild(ctx context.Context, plugins []string) (string, error) {
	output := b.binaryPath + ".new"

	args := make([]string, 0, 3+2*len(plugins))
	args = append(args, "build", "--output", output)
	for _, p := range plugins {
		args = append(args, "--plugin", p)
	}

	cmd := exec.CommandContext(ctx, b.xsclawPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		_ = os.Remove(output) // best-effort cleanup
		return "", fmt.Errorf("xsclaw build failed: %w", err)
	}

	return output, nil
}

// ReExec replaces the current process with the binary at binaryPath.
// On success this function never returns. Uses syscall.Exec (Unix only).
func (b *Bootstrapper) ReExec(binaryPath string) error {
	//nolint:gosec // binaryPath is controlled by the bootstrapper, not user input.
	return syscall.Exec(binaryPath, os.Args, os.Environ())
}
