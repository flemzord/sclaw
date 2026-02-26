package router

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
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

	// maxSessions limits the number of concurrent sessions.
	// Zero means unlimited.
	maxSessions int

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

// SetMaxSessions configures the maximum number of concurrent sessions.
// Zero means unlimited.
func (s *InMemorySessionStore) SetMaxSessions(limit int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxSessions = limit
}

// GetOrCreate returns the existing session for the key, or creates a new
// one if none exists. The bool return is true when a new session was created.
// If maxSessions > 0 and the limit is reached, no new session is created
// and (nil, false) is returned.
func (s *InMemorySessionStore) GetOrCreate(key SessionKey) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sess, ok := s.sessions[key]; ok {
		return sess, false
	}

	// Enforce max sessions if configured.
	if s.maxSessions > 0 && len(s.sessions) >= s.maxSessions {
		return nil, false
	}

	id, err := generateID()
	if err != nil {
		// Wrap and surface the error in the session ID field as a last resort.
		// In practice this should never happen (requires broken OS entropy).
		id = fmt.Sprintf("err-%v", err)
	}

	now := s.now()
	sess := &Session{
		ID:           id,
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

// Range calls fn for each session. If fn returns false, iteration stops.
// The callback receives a snapshot-safe view (the lock is held for the
// entire iteration — keep fn fast).
func (s *InMemorySessionStore) Range(fn func(SessionKey, *Session) bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for key, sess := range s.sessions {
		if !fn(key, sess) {
			return
		}
	}
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
// Returns an error if the OS entropy source is unavailable.
func generateID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("router: crypto/rand unavailable: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}
