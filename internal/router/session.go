package router

import (
	"time"

	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/pkg/message"
)

// SessionKey is the composite key for O(1) session lookups.
// It uniquely identifies a conversation by channel, chat, and thread.
type SessionKey struct {
	Channel  string
	ChatID   string
	ThreadID string
}

// SessionKeyFromMessage derives a SessionKey from an inbound message.
// Messages in the same channel/chat/thread share a session.
func SessionKeyFromMessage(msg message.InboundMessage) SessionKey {
	return SessionKey{
		Channel:  msg.Channel,
		ChatID:   msg.Chat.ID,
		ThreadID: msg.ThreadID,
	}
}

// Session represents an active conversation session.
// It holds the full conversation history and metadata needed by the
// agent loop to produce contextual responses.
type Session struct {
	ID           string
	Key          SessionKey
	AgentID      string
	CreatedAt    time.Time
	LastActiveAt time.Time
	History      []provider.LLMMessage
	Metadata     map[string]any
}

// SessionStore manages session lifecycle.
// Implementations must be safe for concurrent use.
type SessionStore interface {
	// GetOrCreate returns an existing session or creates a new one.
	// The bool return indicates whether the session was newly created.
	GetOrCreate(key SessionKey) (*Session, bool)

	// Get returns the session for the given key, or nil if none exists.
	Get(key SessionKey) *Session

	// Touch updates the session's LastActiveAt timestamp.
	Touch(key SessionKey)

	// Delete removes the session for the given key.
	Delete(key SessionKey)

	// Prune removes sessions that have been idle longer than maxIdle
	// and returns the number of sessions pruned.
	Prune(maxIdle time.Duration) int

	// Len returns the number of active sessions.
	Len() int

	// Range calls fn for each session. If fn returns false, iteration stops.
	Range(fn func(SessionKey, *Session) bool)
}
