package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flemzord/sclaw/internal/memory"
)

// Index stores or updates a fact. If a fact with the same ID exists,
// it is replaced (FTS5 index is updated via triggers).
func (s *factStore) Index(ctx context.Context, fact memory.Fact) error {
	tagsJSON, err := json.Marshal(fact.Tags)
	if err != nil {
		return fmt.Errorf("sqlite: marshal tags: %w", err)
	}

	metaJSON, err := json.Marshal(fact.Metadata)
	if err != nil {
		return fmt.Errorf("sqlite: marshal metadata: %w", err)
	}

	createdAt := fact.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO facts (id, content, source, tags, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		fact.ID, fact.Content, fact.Source,
		string(tagsJSON), string(metaJSON),
		createdAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("sqlite: index fact: %w", err)
	}

	return nil
}

// Search retrieves the top-K facts matching the query using FTS5 full-text search.
func (s *factStore) Search(ctx context.Context, query string, topK int) ([]memory.Fact, error) {
	if query == "" || topK <= 0 {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT f.id, f.content, f.source, f.tags, f.metadata, f.created_at
		FROM facts_fts
		JOIN facts f ON f.rowid = facts_fts.rowid
		WHERE facts_fts MATCH ?
		ORDER BY rank
		LIMIT ?`,
		query, topK,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: search facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanFacts(rows)
}

// SearchByMetadata retrieves facts where metadata[key] == value.
func (s *factStore) SearchByMetadata(ctx context.Context, key, value string) ([]memory.Fact, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, content, source, tags, metadata, created_at
		FROM facts
		WHERE json_extract(metadata, '$.'||?) = ?`,
		key, value,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: search by metadata: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanFacts(rows)
}

// Delete removes a fact by ID. Returns memory.ErrFactNotFound if the fact
// does not exist.
func (s *factStore) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM facts WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("sqlite: delete fact: %w", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: rows affected: %w", err)
	}
	if n == 0 {
		return memory.ErrFactNotFound
	}

	return nil
}

// Len returns the total number of stored facts.
func (s *factStore) Len() int {
	var count int
	if err := s.db.QueryRowContext(context.TODO(), "SELECT COUNT(*) FROM facts").Scan(&count); err != nil {
		s.logger.Error("sqlite: count facts failed", "error", err)
		return 0
	}
	return count
}

func scanFacts(rows *sql.Rows) ([]memory.Fact, error) {
	var facts []memory.Fact
	for rows.Next() {
		var (
			fact         memory.Fact
			tagsJSON     string
			metaJSON     string
			createdAtStr string
		)

		if err := rows.Scan(&fact.ID, &fact.Content, &fact.Source, &tagsJSON, &metaJSON, &createdAtStr); err != nil {
			return nil, fmt.Errorf("sqlite: scan fact: %w", err)
		}

		if tagsJSON != "" && tagsJSON != "[]" && tagsJSON != "null" {
			if err := json.Unmarshal([]byte(tagsJSON), &fact.Tags); err != nil {
				return nil, fmt.Errorf("sqlite: unmarshal tags: %w", err)
			}
		}

		if metaJSON != "" && metaJSON != "{}" && metaJSON != "null" {
			if err := json.Unmarshal([]byte(metaJSON), &fact.Metadata); err != nil {
				return nil, fmt.Errorf("sqlite: unmarshal metadata: %w", err)
			}
		}

		if createdAtStr != "" {
			t, err := time.Parse(time.RFC3339Nano, createdAtStr)
			if err != nil {
				return nil, fmt.Errorf("sqlite: parse created_at %q: %w", createdAtStr, err)
			}
			fact.CreatedAt = t
		}

		facts = append(facts, fact)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: scan facts rows: %w", err)
	}

	return facts, nil
}
