package memory

import (
	"context"
	"time"
)

// Fact is a piece of long-term knowledge extracted from a conversation.
type Fact struct {
	ID        string
	Content   string
	Source    string // session ID where the fact was extracted
	Tags      []string
	Metadata  map[string]string
	CreatedAt time.Time
}

// Store manages long-term memory facts.
// Implementations must be safe for concurrent use.
type Store interface {
	// Index stores a new fact.
	Index(ctx context.Context, fact Fact) error

	// Search retrieves the top-K facts matching the query by content similarity.
	Search(ctx context.Context, query string, topK int) ([]Fact, error)

	// SearchByMetadata retrieves facts where metadata[key] == value.
	SearchByMetadata(ctx context.Context, key, value string) ([]Fact, error)

	// Delete removes a fact by ID.
	Delete(ctx context.Context, id string) error

	// Len returns the total number of stored facts.
	Len() int
}
