package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

const schemaVersion = 1

// schemaStatements are executed in order to create the database schema.
// All use IF NOT EXISTS for idempotent re-application.
var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS messages (
		session_id TEXT    NOT NULL,
		seq        INTEGER NOT NULL,
		role       TEXT    NOT NULL,
		content    TEXT    NOT NULL DEFAULT '',
		name       TEXT    NOT NULL DEFAULT '',
		tool_id    TEXT    NOT NULL DEFAULT '',
		tool_calls TEXT    NOT NULL DEFAULT '[]',
		is_error   INTEGER NOT NULL DEFAULT 0,
		created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		PRIMARY KEY (session_id, seq)
	)`,

	`CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, seq)`,

	`CREATE TABLE IF NOT EXISTS summaries (
		session_id TEXT PRIMARY KEY,
		summary    TEXT NOT NULL,
		updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	)`,

	`CREATE TABLE IF NOT EXISTS facts (
		id         TEXT PRIMARY KEY,
		content    TEXT NOT NULL,
		source     TEXT NOT NULL DEFAULT '',
		tags       TEXT NOT NULL DEFAULT '[]',
		metadata   TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	)`,

	`CREATE VIRTUAL TABLE IF NOT EXISTS facts_fts USING fts5(
		content,
		content=facts,
		content_rowid=rowid
	)`,

	`CREATE TRIGGER IF NOT EXISTS facts_ai AFTER INSERT ON facts BEGIN
		INSERT INTO facts_fts(rowid, content) VALUES (new.rowid, new.content);
	END`,

	`CREATE TRIGGER IF NOT EXISTS facts_ad AFTER DELETE ON facts BEGIN
		INSERT INTO facts_fts(facts_fts, rowid, content) VALUES ('delete', old.rowid, old.content);
	END`,

	`CREATE TRIGGER IF NOT EXISTS facts_au AFTER UPDATE ON facts BEGIN
		INSERT INTO facts_fts(facts_fts, rowid, content) VALUES ('delete', old.rowid, old.content);
		INSERT INTO facts_fts(rowid, content) VALUES (new.rowid, new.content);
	END`,
}

// migrate creates or updates the database schema to the latest version.
// All DDL uses IF NOT EXISTS, making migration idempotent.
func migrate(db *sql.DB) error {
	ctx := context.TODO()

	// Ensure schema_version table exists first.
	if _, err := db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)"); err != nil {
		return fmt.Errorf("sqlite: create schema_version: %w", err)
	}

	var current int
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&current); err != nil {
		return fmt.Errorf("sqlite: read schema version: %w", err)
	}

	if current >= schemaVersion {
		return nil
	}

	for _, stmt := range schemaStatements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("sqlite: migrate: %w\nstatement: %s", err, stmt)
		}
	}

	if _, err := db.ExecContext(ctx, "INSERT OR REPLACE INTO schema_version (version) VALUES (?)", schemaVersion); err != nil {
		return fmt.Errorf("sqlite: record schema version: %w", err)
	}

	return nil
}
