// Package security provides centralized credential management, log redaction,
// rate limiting, input validation, subprocess sanitization, and sandboxing.
package security

import (
	"context"
	"slices"
	"sync"
)

// credentialContextKey is the unexported key for storing CredentialStore in context.
type credentialContextKey struct{}

// CredentialStore is a thread-safe store for sensitive credentials.
// It is the single source of truth for secrets at runtime, replacing
// scattered environment variable reads throughout the codebase.
type CredentialStore struct {
	mu    sync.RWMutex
	creds map[string]string
}

// NewCredentialStore creates an empty credential store.
func NewCredentialStore() *CredentialStore {
	return &CredentialStore{
		creds: make(map[string]string),
	}
}

// Set stores a credential. If a credential with the same name already exists,
// it is overwritten.
func (s *CredentialStore) Set(name, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creds[name] = value
}

// Get returns the credential value and true, or "" and false if not found.
func (s *CredentialStore) Get(name string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.creds[name]
	return v, ok
}

// Has returns true if a credential with the given name exists.
func (s *CredentialStore) Has(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.creds[name]
	return ok
}

// Names returns a sorted list of all credential names.
func (s *CredentialStore) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.creds))
	for name := range s.creds {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Values returns all credential values. Order is not guaranteed.
// This is intended for registering values with a Redactor.
func (s *CredentialStore) Values() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	values := make([]string, 0, len(s.creds))
	for _, v := range s.creds {
		if v != "" {
			values = append(values, v)
		}
	}
	return values
}

// Delete removes a credential by name. It is a no-op if the credential does not exist.
func (s *CredentialStore) Delete(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.creds, name)
}

// Len returns the number of stored credentials.
func (s *CredentialStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.creds)
}

// WithCredentials returns a new context carrying the credential store.
func WithCredentials(ctx context.Context, store *CredentialStore) context.Context {
	return context.WithValue(ctx, credentialContextKey{}, store)
}

// CredentialsFromContext retrieves the credential store from a context.
// Returns nil if no store is present.
func CredentialsFromContext(ctx context.Context) *CredentialStore {
	store, _ := ctx.Value(credentialContextKey{}).(*CredentialStore)
	return store
}
