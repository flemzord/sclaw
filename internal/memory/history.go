// Package memory provides session history and long-term memory storage
// interfaces with in-memory implementations.
package memory

import (
	"time"

	"github.com/flemzord/sclaw/internal/provider"
)

// Exchange represents a single user-assistant exchange in a conversation.
type Exchange struct {
	UserMessage      provider.LLMMessage
	AssistantMessage provider.LLMMessage
	Timestamp        time.Time
}

// HistoryStore manages session conversation history.
// Implementations must be safe for concurrent use.
type HistoryStore interface {
	// Append adds a message to the session's history.
	Append(sessionID string, msg provider.LLMMessage) error

	// GetRecent returns the n most recent messages for a session.
	// If fewer than n messages exist, all messages are returned.
	GetRecent(sessionID string, n int) ([]provider.LLMMessage, error)

	// GetAll returns all messages for a session.
	GetAll(sessionID string) ([]provider.LLMMessage, error)

	// SetSummary stores a compaction summary for a session, replacing any previous one.
	SetSummary(sessionID string, summary string) error

	// GetSummary returns the stored summary for a session.
	// Returns an empty string if no summary exists.
	GetSummary(sessionID string) (string, error)

	// Purge removes all history and summary for a session.
	Purge(sessionID string) error

	// Len returns the number of messages stored for a session.
	Len(sessionID string) (int, error)
}
