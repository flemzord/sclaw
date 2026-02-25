package memory

import (
	"sync"

	"github.com/flemzord/sclaw/internal/provider"
)

// sessionData holds the history and summary for a single session.
type sessionData struct {
	messages []provider.LLMMessage
	summary  string
}

// InMemoryHistoryStore is a thread-safe, in-memory implementation of HistoryStore.
type InMemoryHistoryStore struct {
	mu       sync.RWMutex
	sessions map[string]*sessionData
}

// NewInMemoryHistoryStore creates a new empty history store.
func NewInMemoryHistoryStore() *InMemoryHistoryStore {
	return &InMemoryHistoryStore{
		sessions: make(map[string]*sessionData),
	}
}

// Compile-time interface check.
var _ HistoryStore = (*InMemoryHistoryStore)(nil)

func (s *InMemoryHistoryStore) getOrCreate(sessionID string) *sessionData {
	sd, ok := s.sessions[sessionID]
	if !ok {
		sd = &sessionData{}
		s.sessions[sessionID] = sd
	}
	return sd
}

// Append adds a message to the session's history.
func (s *InMemoryHistoryStore) Append(sessionID string, msg provider.LLMMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sd := s.getOrCreate(sessionID)
	sd.messages = append(sd.messages, msg)
	return nil
}

// GetRecent returns the n most recent messages for a session.
func (s *InMemoryHistoryStore) GetRecent(sessionID string, n int) ([]provider.LLMMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sd, ok := s.sessions[sessionID]
	if !ok {
		return nil, nil
	}

	msgs := sd.messages
	if n >= len(msgs) {
		result := make([]provider.LLMMessage, len(msgs))
		copy(result, msgs)
		return result, nil
	}

	start := len(msgs) - n
	result := make([]provider.LLMMessage, n)
	copy(result, msgs[start:])
	return result, nil
}

// GetAll returns all messages for a session.
func (s *InMemoryHistoryStore) GetAll(sessionID string) ([]provider.LLMMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sd, ok := s.sessions[sessionID]
	if !ok {
		return nil, nil
	}

	result := make([]provider.LLMMessage, len(sd.messages))
	copy(result, sd.messages)
	return result, nil
}

// SetSummary stores a compaction summary for a session.
func (s *InMemoryHistoryStore) SetSummary(sessionID string, summary string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sd := s.getOrCreate(sessionID)
	sd.summary = summary
	return nil
}

// GetSummary returns the stored summary for a session.
func (s *InMemoryHistoryStore) GetSummary(sessionID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sd, ok := s.sessions[sessionID]
	if !ok {
		return "", nil
	}
	return sd.summary, nil
}

// Purge removes all history and summary for a session.
func (s *InMemoryHistoryStore) Purge(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
	return nil
}

// Len returns the number of messages stored for a session.
func (s *InMemoryHistoryStore) Len(sessionID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sd, ok := s.sessions[sessionID]
	if !ok {
		return 0, nil
	}
	return len(sd.messages), nil
}
