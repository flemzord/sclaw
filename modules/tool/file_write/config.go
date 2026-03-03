// Package filewrite provides a configurable file write tool module for sclaw.
// It migrates the builtin write_file tool into a module with configurable
// max file size, directory creation, and approval policy.
package filewrite

import "fmt"

const defaultMaxFileSize = 1 << 20 // 1 MiB

// Config holds the tool.file_write module configuration.
type Config struct {
	// MaxFileSize is the maximum content size in bytes to write. Defaults to 1 MiB.
	MaxFileSize int `yaml:"max_file_size"`

	// CreateDirs enables automatic parent directory creation. Defaults to true.
	CreateDirs *bool `yaml:"create_dirs"`

	// DefaultPolicy is the default approval level: "allow", "ask", or "deny". Defaults to "allow".
	DefaultPolicy string `yaml:"default_policy"`
}

func (c *Config) defaults() {
	if c.MaxFileSize == 0 {
		c.MaxFileSize = defaultMaxFileSize
	}
	if c.CreateDirs == nil {
		t := true
		c.CreateDirs = &t
	}
	if c.DefaultPolicy == "" {
		c.DefaultPolicy = "allow"
	}
}

func (c *Config) createDirsEnabled() bool {
	return c.CreateDirs == nil || *c.CreateDirs
}

func (c *Config) validate() error {
	if c.MaxFileSize <= 0 {
		return fmt.Errorf("file_write: max_file_size must be positive, got %d", c.MaxFileSize)
	}
	switch c.DefaultPolicy {
	case "allow", "ask", "deny":
	default:
		return fmt.Errorf("file_write: invalid default_policy %q (must be allow, ask, or deny)", c.DefaultPolicy)
	}
	return nil
}
