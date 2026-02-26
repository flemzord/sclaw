package gateway

import (
	"context"
	"testing"

	"github.com/flemzord/sclaw/internal/provider"
)

// fakeProvider is a minimal test provider that always fails (for health testing).
type fakeProvider struct {
	name    string
	failErr error
}

func (p *fakeProvider) Complete(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
	if p.failErr != nil {
		return provider.CompletionResponse{}, p.failErr
	}
	return provider.CompletionResponse{Content: p.name}, nil
}

func (p *fakeProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	if p.failErr != nil {
		return nil, p.failErr
	}
	ch := make(chan provider.StreamChunk, 1)
	ch <- provider.StreamChunk{Content: p.name}
	close(ch)
	return ch, nil
}

func (p *fakeProvider) ContextWindowSize() int { return 4096 }
func (p *fakeProvider) ModelName() string      { return p.name }

func (p *fakeProvider) HealthCheck(_ context.Context) error {
	return p.failErr
}

// newTestChain creates a Chain for testing.
func newTestChain(t *testing.T, entries []provider.ChainEntry) *provider.Chain {
	t.Helper()
	chain, err := provider.NewChain(entries)
	if err != nil {
		t.Fatalf("NewChain: %v", err)
	}
	return chain
}
