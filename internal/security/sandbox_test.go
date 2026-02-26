package security

import (
	"testing"
	"time"
)

func TestSandboxPolicy_ShouldSandbox(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		policy SandboxPolicy
		scopes []string
		want   bool
	}{
		{
			name:   "disabled policy",
			policy: SandboxPolicy{Enabled: false},
			scopes: []string{"exec"},
			want:   false,
		},
		{
			name:   "enabled with exec scope",
			policy: SandboxPolicy{Enabled: true},
			scopes: []string{"exec"},
			want:   true,
		},
		{
			name:   "enabled with read_write scope",
			policy: SandboxPolicy{Enabled: true},
			scopes: []string{"read_write"},
			want:   true,
		},
		{
			name:   "enabled with read_only scope",
			policy: SandboxPolicy{Enabled: true},
			scopes: []string{"read_only"},
			want:   false,
		},
		{
			name:   "enabled with network scope",
			policy: SandboxPolicy{Enabled: true},
			scopes: []string{"network"},
			want:   false,
		},
		{
			name:   "custom scopes",
			policy: SandboxPolicy{Enabled: true, ScopesRequiringSandbox: []string{"network"}},
			scopes: []string{"network"},
			want:   true,
		},
		{
			name:   "custom scopes - no match",
			policy: SandboxPolicy{Enabled: true, ScopesRequiringSandbox: []string{"network"}},
			scopes: []string{"exec"},
			want:   false,
		},
		{
			name:   "multiple scopes - one matches",
			policy: SandboxPolicy{Enabled: true},
			scopes: []string{"read_only", "exec"},
			want:   true,
		},
		{
			name:   "empty scopes",
			policy: SandboxPolicy{Enabled: true},
			scopes: nil,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.policy.ShouldSandbox(tt.scopes)
			if got != tt.want {
				t.Errorf("ShouldSandbox(%v) = %v, want %v", tt.scopes, got, tt.want)
			}
		})
	}
}

func TestResourceLimitsDefaults(t *testing.T) {
	t.Parallel()

	defaults := resourceLimitsDefaults()
	if defaults.CPUShares != 512 {
		t.Errorf("CPUShares = %d, want 512", defaults.CPUShares)
	}
	if defaults.MemoryMB != 256 {
		t.Errorf("MemoryMB = %d, want 256", defaults.MemoryMB)
	}
	if defaults.DiskMB != 100 {
		t.Errorf("DiskMB = %d, want 100", defaults.DiskMB)
	}
	if defaults.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", defaults.Timeout)
	}
}

func TestNewSandboxExecutor_Defaults(t *testing.T) {
	t.Parallel()

	se := NewSandboxExecutor(SandboxPolicy{Enabled: true}, ResourceLimits{}, "")

	if se.limits.CPUShares != 512 {
		t.Errorf("CPUShares = %d, want 512", se.limits.CPUShares)
	}
	if se.image != "alpine:latest" {
		t.Errorf("image = %q, want alpine:latest", se.image)
	}
}

func TestNewSandboxExecutor_CustomImage(t *testing.T) {
	t.Parallel()

	se := NewSandboxExecutor(SandboxPolicy{Enabled: true}, ResourceLimits{}, "ubuntu:22.04")

	if se.image != "ubuntu:22.04" {
		t.Errorf("image = %q, want ubuntu:22.04", se.image)
	}
}
