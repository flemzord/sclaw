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
// plugins. On success, it atomically replaces the current binary so that
// service restarts pick up the new version. Returns the path to the binary.
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

	// Replace the original binary so that service restarts use the new version.
	if err := os.Rename(output, b.binaryPath); err != nil {
		return "", fmt.Errorf("replacing binary: %w", err)
	}

	return b.binaryPath, nil
}

// ReExec replaces the current process with the binary at binaryPath.
// On success this function never returns. Uses syscall.Exec (Unix only).
//
// Design: ReExec passes the full environment because the new process is
// the same binary with the same trust level (self-to-self replacement).
// Credential env vars (OPENAI_API_KEY, etc.) are intentionally preserved
// so the new process can start without config-loading failures.
// Use security.SanitizedEnv() only for untrusted subprocesses (tools, plugins).
func (b *Bootstrapper) ReExec(binaryPath string) error {
	//nolint:gosec // binaryPath is controlled by the bootstrapper, not user input.
	return syscall.Exec(binaryPath, os.Args, os.Environ())
}
