package security

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// SandboxPolicy defines when and how tools should be sandboxed.
type SandboxPolicy struct {
	// Enabled activates sandboxing. When false, no sandboxing occurs.
	Enabled bool `yaml:"enabled"`

	// ScopesRequiringSandbox lists tool scopes that trigger sandboxing.
	// Defaults to ["exec", "read_write"].
	ScopesRequiringSandbox []string `yaml:"scopes_requiring_sandbox"`
}

// sandboxDefaultScopes are the default scopes that require sandboxing.
var sandboxDefaultScopes = []string{"exec", "read_write"}

// ShouldSandbox returns true if the given scopes require sandboxing
// according to this policy.
func (p SandboxPolicy) ShouldSandbox(scopes []string) bool {
	if !p.Enabled {
		return false
	}

	required := p.ScopesRequiringSandbox
	if len(required) == 0 {
		required = sandboxDefaultScopes
	}

	for _, scope := range scopes {
		if slices.Contains(required, scope) {
			return true
		}
	}
	return false
}

// ResourceLimits defines resource constraints for sandboxed execution.
type ResourceLimits struct {
	// CPUShares is the relative CPU weight (Docker --cpu-shares).
	CPUShares int `yaml:"cpu_shares"`

	// MemoryMB is the memory limit in megabytes (Docker --memory).
	MemoryMB int `yaml:"memory_mb"`

	// DiskMB is the disk size limit in megabytes (Docker --storage-opt size).
	DiskMB int `yaml:"disk_mb"`

	// Timeout is the maximum execution duration.
	Timeout time.Duration `yaml:"timeout"`
}

// resourceLimitsDefaults returns sane defaults for sandbox limits.
func resourceLimitsDefaults() ResourceLimits {
	return ResourceLimits{
		CPUShares: 512,
		MemoryMB:  256,
		DiskMB:    100,
		Timeout:   30 * time.Second,
	}
}

// SandboxExecutor wraps command execution in a Docker container when
// sandboxing is required. If Docker is not available, it returns an error
// rather than running unsandboxed (fail-closed).
type SandboxExecutor struct {
	policy SandboxPolicy
	limits ResourceLimits
	image  string // Docker image to use
}

// NewSandboxExecutor creates a sandbox executor with the given policy and limits.
// Zero-value limits are replaced with defaults.
func NewSandboxExecutor(policy SandboxPolicy, limits ResourceLimits, image string) *SandboxExecutor {
	defaults := resourceLimitsDefaults()
	if limits.CPUShares <= 0 {
		limits.CPUShares = defaults.CPUShares
	}
	if limits.MemoryMB <= 0 {
		limits.MemoryMB = defaults.MemoryMB
	}
	if limits.DiskMB <= 0 {
		limits.DiskMB = defaults.DiskMB
	}
	if limits.Timeout <= 0 {
		limits.Timeout = defaults.Timeout
	}
	if image == "" {
		image = "alpine:3.19"
	}

	return &SandboxExecutor{
		policy: policy,
		limits: limits,
		image:  image,
	}
}

// Execute runs a command in a sandboxed Docker container.
// Returns the combined stdout/stderr output or an error.
func (s *SandboxExecutor) Execute(ctx context.Context, command string, workdir string, env []string) ([]byte, error) {
	if !s.policy.Enabled {
		return nil, fmt.Errorf("sandbox: policy not enabled")
	}

	timeout := s.limits.Timeout
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	if workdir != "" {
		cleaned := filepath.Clean(workdir)
		if strings.Contains(cleaned, ":") {
			return nil, fmt.Errorf("sandbox: workdir contains invalid character: %q", workdir)
		}
	}

	args := []string{
		"run", "--rm",
		"--read-only",
		"--network=none",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges:true",
		"--user", "65534:65534",
		"--pids-limit", "256",
		"--cpu-shares", strconv.Itoa(s.limits.CPUShares),
		"--memory", strconv.Itoa(s.limits.MemoryMB) + "m",
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=" + strconv.Itoa(s.limits.DiskMB) + "m",
	}

	if workdir != "" {
		args = append(args, "-v", workdir+":/workspace:ro", "-w", "/workspace")
	}

	for _, e := range env {
		args = append(args, "-e", e)
	}

	args = append(args, s.image, "sh", "-c", command)

	//nolint:gosec // args are constructed programmatically from validated input.
	cmd := exec.CommandContext(ctx, "docker", args...)
	return cmd.CombinedOutput()
}

// IsDockerAvailable checks if the docker CLI is available on PATH.
func IsDockerAvailable() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}
