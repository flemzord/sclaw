package router

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// InMemorySessionStore is a concurrency-safe, in-memory SessionStore.
// It uses a map with a read-write mutex for O(1) lookups. The `now`
// function is injectable for deterministic testing (same pattern as
// healthTracker in internal/provider).
type InMemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[SessionKey]*Session

	// now is injectable for testing. Defaults to time.Now.
	now func() time.Time
}

// NewInMemorySessionStore creates a ready-to-use in-memory session store.
func NewInMemorySessionStore() *InMemorySessionStore {
	return &InMemorySessionStore{
		sessions: make(map[SessionKey]*Session),
		now:      time.Now,
	}
}

// GetOrCreate returns the existing session for the key, or creates a new
// one if none exists. The bool return is true when a new session was created.
func (s *InMemorySessionStore) GetOrCreate(key SessionKey) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sess, ok := s.sessions[key]; ok {
		return sess, false
	}

	now := s.now()
	sess := &Session{
		ID:           generateID(),
		Key:          key,
		CreatedAt:    now,
		LastActiveAt: now,
		// History and Metadata left as nil slices/maps (idiomatic Go).
	}
	s.sessions[key] = sess
	return sess, true
}

// Get returns the session for the given key, or nil if none exists.
func (s *InMemorySessionStore) Get(key SessionKey) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[key]
}

// Touch updates the session's LastActiveAt timestamp to the current time.
// It is a no-op if the session does not exist.
func (s *InMemorySessionStore) Touch(key SessionKey) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sess, ok := s.sessions[key]; ok {
		sess.LastActiveAt = s.now()
	}
}

// Delete removes the session for the given key. It is a no-op if the
// session does not exist.
func (s *InMemorySessionStore) Delete(key SessionKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, key)
}

// Prune removes sessions whose idle time exceeds maxIdle and returns the
// number of sessions pruned. This is intended to be called periodically
// by a background goroutine.
func (s *InMemorySessionStore) Prune(maxIdle time.Duration) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	pruned := 0
	for key, sess := range s.sessions {
		if now.Sub(sess.LastActiveAt) > maxIdle {
			delete(s.sessions, key)
			pruned++
		}
	}
	return pruned
}

// Len returns the number of active sessions.
func (s *InMemorySessionStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// ActiveKeys returns a snapshot of currently active session keys.
func (s *InMemorySessionStore) ActiveKeys() map[SessionKey]struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := make(map[SessionKey]struct{}, len(s.sessions))
	for key := range s.sessions {
		keys[key] = struct{}{}
	}
	return keys
}

// generateID produces a 32-character hex string from 16 random bytes.
// It uses crypto/rand for uniqueness without external dependencies.
func generateID() string {
	var buf [16]byte
	// crypto/rand.Read always returns len(buf) bytes on supported platforms.
	// A failure here indicates a broken OS entropy source — a condition so
	// severe that no reasonable recovery exists.
	if _, err := rand.Read(buf[:]); err != nil {
		panic("router: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(buf[:])
}
