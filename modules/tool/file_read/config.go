// Package fileread provides a configurable file read tool module for sclaw.
// It migrates the builtin read_file tool into a module with configurable
// max file size and approval policy.
package fileread

import "fmt"

const defaultMaxFileSize = 1 << 20 // 1 MiB

// Config holds the tool.file_read module configuration.
type Config struct {
	// MaxFileSize is the maximum file size in bytes to read. Defaults to 1 MiB.
	MaxFileSize int `yaml:"max_file_size"`

	// DefaultPolicy is the default approval level: "allow", "ask", or "deny". Defaults to "allow".
	DefaultPolicy string `yaml:"default_policy"`
}

func (c *Config) defaults() {
	if c.MaxFileSize == 0 {
		c.MaxFileSize = defaultMaxFileSize
	}
	if c.DefaultPolicy == "" {
		c.DefaultPolicy = "allow"
	}
}

func (c *Config) validate() error {
	if c.MaxFileSize <= 0 {
		return fmt.Errorf("file_read: max_file_size must be positive, got %d", c.MaxFileSize)
	}
	switch c.DefaultPolicy {
	case "allow", "ask", "deny":
	default:
		return fmt.Errorf("file_read: invalid default_policy %q (must be allow, ask, or deny)", c.DefaultPolicy)
	}
	return nil
}
