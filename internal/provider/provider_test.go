package provider_test

import (
	"context"
	"testing"

	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/provider/providertest"
)

// Interface guards.
var (
	_ provider.Provider      = (*providertest.MockProvider)(nil)
	_ provider.HealthChecker = (*providertest.MockProvider)(nil)
)

func TestMockProviderSatisfiesInterface(t *testing.T) {
	t.Parallel()

	mock := &providertest.MockProvider{
		CompleteFunc: func(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
			return provider.CompletionResponse{Content: "ok"}, nil
		},
		ContextWindowSizeFunc: func() int { return 4096 },
		ModelNameFunc:         func() string { return "test-model" },
	}

	resp, err := mock.Complete(context.Background(), provider.CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("content = %q, want %q", resp.Content, "ok")
	}
	if mock.ModelName() != "test-model" {
		t.Errorf("ModelName() = %q, want %q", mock.ModelName(), "test-model")
	}
	if mock.ContextWindowSize() != 4096 {
		t.Errorf("ContextWindowSize() = %d, want %d", mock.ContextWindowSize(), 4096)
	}
}
