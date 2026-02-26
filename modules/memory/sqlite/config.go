package sqlite

import "fmt"

const (
	defaultBusyTimeout = 5000
	defaultDBFile      = "memory.db"
)

// Config holds the SQLite memory module configuration.
type Config struct {
	// Path is the database file path. Defaults to {DataDir}/memory.db.
	Path string `yaml:"path"`

	// WAL enables WAL journal mode for concurrent reads. Defaults to true.
	WAL *bool `yaml:"wal"`

	// BusyTimeout is the milliseconds to wait on a busy lock. Defaults to 5000.
	BusyTimeout int `yaml:"busy_timeout"`
}

func (c *Config) defaults() {
	if c.WAL == nil {
		t := true
		c.WAL = &t
	}
	if c.BusyTimeout == 0 {
		c.BusyTimeout = defaultBusyTimeout
	}
}

func (c *Config) walEnabled() bool {
	return c.WAL == nil || *c.WAL
}

func (c *Config) validate() error {
	if c.BusyTimeout < 0 {
		return fmt.Errorf("sqlite: busy_timeout must be non-negative, got %d", c.BusyTimeout)
	}
	return nil
}
