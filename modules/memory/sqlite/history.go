package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/flemzord/sclaw/internal/provider"
)

// Append adds a message to the session's history.
func (h *historyStore) Append(sessionID string, msg provider.LLMMessage) error {
	var toolCallsJSON []byte
	if len(msg.ToolCalls) > 0 {
		var err error
		toolCallsJSON, err = json.Marshal(msg.ToolCalls)
		if err != nil {
			return fmt.Errorf("sqlite: marshal tool_calls: %w", err)
		}
	} else {
		toolCallsJSON = []byte("[]")
	}

	isError := 0
	if msg.IsError {
		isError = 1
	}

	// HistoryStore interface does not carry context; use TODO as placeholder.
	_, err := h.db.ExecContext(context.TODO(), `
		INSERT INTO messages (session_id, seq, role, content, name, tool_id, tool_calls, is_error)
		VALUES (?, COALESCE((SELECT MAX(seq) FROM messages WHERE session_id = ?), 0) + 1,
		        ?, ?, ?, ?, ?, ?)`,
		sessionID, sessionID,
		string(msg.Role), msg.Content, msg.Name, msg.ToolID, string(toolCallsJSON), isError,
	)
	if err != nil {
		return fmt.Errorf("sqlite: append message: %w", err)
	}

	return nil
}

// GetRecent returns the n most recent messages for a session.
func (h *historyStore) GetRecent(sessionID string, n int) ([]provider.LLMMessage, error) {
	if n <= 0 {
		return nil, nil
	}

	rows, err := h.db.QueryContext(context.TODO(), `
		SELECT role, content, name, tool_id, tool_calls, is_error
		FROM messages
		WHERE session_id = ?
		ORDER BY seq DESC
		LIMIT ?`,
		sessionID, n,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: get recent: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var msgs []provider.LLMMessage
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: get recent rows: %w", err)
	}

	// Reverse to chronological order.
	slices.Reverse(msgs)
	return msgs, nil
}

// GetAll returns all messages for a session in chronological order.
func (h *historyStore) GetAll(sessionID string) ([]provider.LLMMessage, error) {
	rows, err := h.db.QueryContext(context.TODO(), `
		SELECT role, content, name, tool_id, tool_calls, is_error
		FROM messages
		WHERE session_id = ?
		ORDER BY seq ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: get all: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var msgs []provider.LLMMessage
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: get all rows: %w", err)
	}

	return msgs, nil
}

// SetSummary stores a compaction summary for a session, replacing any previous one.
func (h *historyStore) SetSummary(sessionID string, summary string) error {
	_, err := h.db.ExecContext(context.TODO(), `
		INSERT OR REPLACE INTO summaries (session_id, summary, updated_at)
		VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))`,
		sessionID, summary,
	)
	if err != nil {
		return fmt.Errorf("sqlite: set summary: %w", err)
	}
	return nil
}

// GetSummary returns the stored summary for a session.
// Returns an empty string if no summary exists.
func (h *historyStore) GetSummary(sessionID string) (string, error) {
	var summary string
	err := h.db.QueryRowContext(context.TODO(),
		"SELECT summary FROM summaries WHERE session_id = ?", sessionID,
	).Scan(&summary)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("sqlite: get summary: %w", err)
	}
	return summary, nil
}

// Purge removes all history and summary for a session.
func (h *historyStore) Purge(sessionID string) error {
	tx, err := h.db.BeginTx(context.TODO(), nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin purge tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(context.TODO(), "DELETE FROM messages WHERE session_id = ?", sessionID); err != nil {
		return fmt.Errorf("sqlite: purge messages: %w", err)
	}
	if _, err := tx.ExecContext(context.TODO(), "DELETE FROM summaries WHERE session_id = ?", sessionID); err != nil {
		return fmt.Errorf("sqlite: purge summaries: %w", err)
	}

	return tx.Commit()
}

// Len returns the number of messages stored for a session.
func (h *historyStore) Len(sessionID string) (int, error) {
	var count int
	err := h.db.QueryRowContext(context.TODO(),
		"SELECT COUNT(*) FROM messages WHERE session_id = ?", sessionID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("sqlite: count messages: %w", err)
	}
	return count, nil
}

// scanner abstracts *sql.Row and *sql.Rows for shared scan logic.
type scanner interface {
	Scan(dest ...any) error
}

func scanMessage(s scanner) (provider.LLMMessage, error) {
	var (
		msg           provider.LLMMessage
		role          string
		toolCallsJSON string
		isError       int
	)

	if err := s.Scan(&role, &msg.Content, &msg.Name, &msg.ToolID, &toolCallsJSON, &isError); err != nil {
		return msg, fmt.Errorf("sqlite: scan message: %w", err)
	}

	msg.Role = provider.MessageRole(role)
	msg.IsError = isError != 0

	if toolCallsJSON != "" && toolCallsJSON != "[]" {
		if err := json.Unmarshal([]byte(toolCallsJSON), &msg.ToolCalls); err != nil {
			return msg, fmt.Errorf("sqlite: unmarshal tool_calls: %w", err)
		}
	}

	return msg, nil
}
