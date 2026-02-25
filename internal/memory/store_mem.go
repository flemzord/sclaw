package memory

import (
	"context"
	"errors"
	"strings"
	"sync"
)

// ErrFactNotFound indicates the requested fact does not exist.
var ErrFactNotFound = errors.New("memory: fact not found")

// InMemoryStore is a thread-safe, in-memory implementation of Store.
// Search uses simple substring matching; a production implementation would
// use vector embeddings.
type InMemoryStore struct {
	mu    sync.RWMutex
	facts []Fact
	index map[string]int // id → index in facts slice
}

// NewInMemoryStore creates a new empty memory store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		index: make(map[string]int),
	}
}

// Compile-time interface check.
var _ Store = (*InMemoryStore)(nil)

// Index stores a new fact.
func (s *InMemoryStore) Index(_ context.Context, fact Fact) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.index[fact.ID]; exists {
		// Update in place.
		s.facts[s.index[fact.ID]] = fact
		return nil
	}

	s.index[fact.ID] = len(s.facts)
	s.facts = append(s.facts, fact)
	return nil
}

// Search retrieves the top-K facts matching the query by substring match.
func (s *InMemoryStore) Search(_ context.Context, query string, topK int) ([]Fact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if topK <= 0 {
		return nil, nil
	}

	queryLower := strings.ToLower(query)
	var results []Fact

	for i := range s.facts {
		if strings.Contains(strings.ToLower(s.facts[i].Content), queryLower) {
			results = append(results, s.facts[i])
			if len(results) >= topK {
				break
			}
		}
	}

	return results, nil
}

// SearchByMetadata retrieves facts where metadata[key] == value.
func (s *InMemoryStore) SearchByMetadata(_ context.Context, key, value string) ([]Fact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []Fact
	for i := range s.facts {
		if s.facts[i].Metadata != nil && s.facts[i].Metadata[key] == value {
			results = append(results, s.facts[i])
		}
	}

	return results, nil
}

// Delete removes a fact by ID.
func (s *InMemoryStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx, ok := s.index[id]
	if !ok {
		return ErrFactNotFound
	}

	// Replace with last element and shrink (swap-delete).
	last := len(s.facts) - 1
	if idx != last {
		s.facts[idx] = s.facts[last]
		s.index[s.facts[idx].ID] = idx
	}
	s.facts = s.facts[:last]
	delete(s.index, id)

	return nil
}

// Len returns the total number of stored facts.
func (s *InMemoryStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.facts)
}
