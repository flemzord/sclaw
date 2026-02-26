package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/flemzord/sclaw/internal/memory"

	_ "modernc.org/sqlite" // SQLite driver registration
)

// OpenHistoryStore opens a SQLite database at the given path and returns
// a HistoryStore backed by it. The caller is responsible for closing the
// returned *sql.DB when done.
//
// The database is created with WAL mode, a 5 s busy timeout, and a single
// connection (SQLite serialises writes). The schema is migrated automatically.
func OpenHistoryStore(path string) (memory.HistoryStore, *sql.DB, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, nil, fmt.Errorf("sqlite: create directory %s: %w", dir, err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, nil, fmt.Errorf("sqlite: open %s: %w", path, err)
	}

	db.SetMaxOpenConns(1)

	ctx := context.TODO()

	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("sqlite: enable WAL: %w", err)
	}

	if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout=%d", defaultBusyTimeout)); err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("sqlite: set busy_timeout: %w", err)
	}

	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, nil, err
	}

	return &historyStore{db: db}, db, nil
}
