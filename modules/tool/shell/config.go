// Package shell implements a configurable shell execution tool module
// that replaces the built-in exec tool.
package shell

import (
	"fmt"
	"time"
)

const (
	defaultTimeout    = 30 * time.Second
	defaultMaxTimeout = 10 * time.Minute
	defaultMaxOutput  = 1 << 20 // 1 MiB
)

// Config holds the tool.shell module configuration.
type Config struct {
	// Timeout is the default command timeout (e.g. "30s"). Defaults to 30s.
	Timeout string `yaml:"timeout"`

	// MaxTimeout is the maximum allowed timeout (e.g. "10m"). Defaults to 10m.
	MaxTimeout string `yaml:"max_timeout"`

	// MaxOutputSize is the max stdout+stderr captured in bytes. Defaults to 1 MiB.
	MaxOutputSize int `yaml:"max_output_size"`

	// DefaultPolicy is the default approval level: "allow", "ask", or "deny". Defaults to "allow".
	DefaultPolicy string `yaml:"default_policy"`
}

func (c *Config) defaults() {
	if c.Timeout == "" {
		c.Timeout = defaultTimeout.String()
	}
	if c.MaxTimeout == "" {
		c.MaxTimeout = defaultMaxTimeout.String()
	}
	if c.MaxOutputSize == 0 {
		c.MaxOutputSize = defaultMaxOutput
	}
	if c.DefaultPolicy == "" {
		c.DefaultPolicy = "allow"
	}
}

func (c *Config) validate() error {
	if _, err := time.ParseDuration(c.Timeout); err != nil {
		return fmt.Errorf("shell: invalid timeout %q: %w", c.Timeout, err)
	}
	if _, err := time.ParseDuration(c.MaxTimeout); err != nil {
		return fmt.Errorf("shell: invalid max_timeout %q: %w", c.MaxTimeout, err)
	}
	if c.MaxOutputSize <= 0 {
		return fmt.Errorf("shell: max_output_size must be positive, got %d", c.MaxOutputSize)
	}
	switch c.DefaultPolicy {
	case "allow", "ask", "deny":
	default:
		return fmt.Errorf("shell: invalid default_policy %q (must be allow, ask, or deny)", c.DefaultPolicy)
	}
	return nil
}

func (c *Config) timeoutDuration() time.Duration {
	d, _ := time.ParseDuration(c.Timeout)
	return d
}

func (c *Config) maxTimeoutDuration() time.Duration {
	d, _ := time.ParseDuration(c.MaxTimeout)
	return d
}
