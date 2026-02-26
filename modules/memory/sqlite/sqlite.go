// Package sqlite implements a persistent SQLite-backed memory module
// providing both HistoryStore and Store interfaces. It uses modernc.org/sqlite
// (pure Go, no CGO) with FTS5 full-text search and WAL mode.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/memory"
	"gopkg.in/yaml.v3"

	_ "modernc.org/sqlite" // SQLite driver registration
)

func init() {
	core.RegisterModule(&Module{})
}

// Compile-time interface guards.
var (
	_ memory.HistoryStore = (*historyStore)(nil)
	_ memory.Store        = (*factStore)(nil)
	_ core.Configurable   = (*Module)(nil)
	_ core.Provisioner    = (*Module)(nil)
	_ core.Validator      = (*Module)(nil)
	_ core.Stopper        = (*Module)(nil)
)

// Module implements a SQLite-backed memory module providing both
// HistoryStore and Store interfaces backed by a single database.
type Module struct {
	config  Config
	db      *sql.DB
	logger  *slog.Logger
	history *historyStore
	store   *factStore
}

// historyStore implements memory.HistoryStore backed by SQLite.
type historyStore struct {
	db *sql.DB
}

// factStore implements memory.Store backed by SQLite with FTS5.
type factStore struct {
	db     *sql.DB
	logger *slog.Logger
}

// ModuleInfo implements core.Module.
func (m *Module) ModuleInfo() core.ModuleInfo {
	return core.ModuleInfo{
		ID:  "memory.sqlite",
		New: func() core.Module { return &Module{} },
	}
}

// Configure implements core.Configurable.
func (m *Module) Configure(node *yaml.Node) error {
	if err := node.Decode(&m.config); err != nil {
		return fmt.Errorf("sqlite: decode config: %w", err)
	}
	m.config.defaults()
	return nil
}

// Provision implements core.Provisioner.
func (m *Module) Provision(ctx *core.AppContext) error {
	m.config.defaults()
	m.logger = ctx.Logger

	if m.config.Path == "" {
		m.config.Path = filepath.Join(ctx.DataDir, defaultDBFile)
	}

	if dir := filepath.Dir(m.config.Path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("sqlite: create directory %s: %w", dir, err)
		}
	}

	db, err := sql.Open("sqlite", m.config.Path)
	if err != nil {
		return fmt.Errorf("sqlite: open %s: %w", m.config.Path, err)
	}

	// SQLite handles one writer at a time; limit pool to 1 connection
	// so PRAGMAs apply consistently.
	db.SetMaxOpenConns(1)

	if m.config.walEnabled() {
		if _, err := db.ExecContext(context.TODO(), "PRAGMA journal_mode=WAL"); err != nil {
			_ = db.Close()
			return fmt.Errorf("sqlite: enable WAL: %w", err)
		}
	}

	if _, err := db.ExecContext(context.TODO(), fmt.Sprintf("PRAGMA busy_timeout=%d", m.config.BusyTimeout)); err != nil {
		_ = db.Close()
		return fmt.Errorf("sqlite: set busy_timeout: %w", err)
	}

	if err := migrate(db); err != nil {
		_ = db.Close()
		return err
	}

	m.db = db
	m.history = &historyStore{db: db}
	m.store = &factStore{db: db, logger: ctx.Logger}

	ctx.RegisterService("memory.history", m.history)
	ctx.RegisterService("memory.store", m.store)

	m.logger.Info("sqlite memory module provisioned",
		"path", m.config.Path,
		"wal", m.config.walEnabled(),
	)

	return nil
}

// Validate implements core.Validator.
func (m *Module) Validate() error {
	if err := m.config.validate(); err != nil {
		return err
	}

	if err := m.db.PingContext(context.TODO()); err != nil {
		return fmt.Errorf("sqlite: ping failed: %w", err)
	}

	// Verify FTS5 virtual table is accessible.
	var n int
	if err := m.db.QueryRowContext(context.TODO(), "SELECT count(*) FROM facts_fts").Scan(&n); err != nil {
		return fmt.Errorf("sqlite: FTS5 not available: %w", err)
	}

	return nil
}

// Stop implements core.Stopper.
func (m *Module) Stop(_ context.Context) error {
	m.logger.Info("sqlite memory module stopping")
	if m.db != nil {
		return m.db.Close()
	}
	return nil
}

// History returns the HistoryStore implementation.
func (m *Module) History() memory.HistoryStore {
	return m.history
}

// Store returns the Store implementation.
func (m *Module) Store() memory.Store {
	return m.store
}
