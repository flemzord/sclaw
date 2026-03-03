package shell

import (
	"testing"

	"github.com/flemzord/sclaw/internal/tool"
)

func TestConfigDefaults(t *testing.T) {
	t.Parallel()

	var c Config
	c.defaults()

	if c.Timeout != "30s" {
		t.Errorf("Timeout = %q, want %q", c.Timeout, "30s")
	}
	if c.MaxTimeout != "10m0s" {
		t.Errorf("MaxTimeout = %q, want %q", c.MaxTimeout, "10m0s")
	}
	if c.MaxOutputSize != 1<<20 {
		t.Errorf("MaxOutputSize = %d, want %d", c.MaxOutputSize, 1<<20)
	}
	if c.DefaultPolicy != "allow" {
		t.Errorf("DefaultPolicy = %q, want %q", c.DefaultPolicy, "allow")
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid defaults",
			cfg: func() Config {
				var c Config
				c.defaults()
				return c
			}(),
		},
		{
			name:    "invalid timeout",
			cfg:     Config{Timeout: "not-a-duration", MaxTimeout: "10m", MaxOutputSize: 1024, DefaultPolicy: "allow"},
			wantErr: true,
		},
		{
			name:    "invalid policy",
			cfg:     Config{Timeout: "30s", MaxTimeout: "10m", MaxOutputSize: 1024, DefaultPolicy: "invalid"},
			wantErr: true,
		},
		{
			name:    "negative max output",
			cfg:     Config{Timeout: "30s", MaxTimeout: "10m", MaxOutputSize: -1, DefaultPolicy: "allow"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.cfg.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestModuleToolProvider(t *testing.T) {
	t.Parallel()

	m := &Module{}
	m.config.defaults()
	_ = m.Provision(nil)

	var tp tool.Provider = m
	tools := tp.Tools()
	if len(tools) != 1 {
		t.Fatalf("Tools() returned %d tools, want 1", len(tools))
	}
	if tools[0].Name() != "exec" {
		t.Errorf("tool name = %q, want %q", tools[0].Name(), "exec")
	}
}
