//go:build integration

package anthropic

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/provider"
)

// Integration tests require ANTHROPIC_API_KEY to be set.
// Run with: go test -tags=integration ./modules/provider/anthropic/...

func TestIntegration_Complete(t *testing.T) {
	a := integrationProvider(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := a.Complete(ctx, provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Say 'hello' and nothing else."},
		},
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	if !strings.Contains(strings.ToLower(resp.Content), "hello") {
		t.Errorf("expected content containing 'hello', got %q", resp.Content)
	}
	if resp.Usage.TotalTokens == 0 {
		t.Error("expected non-zero token usage")
	}
}

func TestIntegration_Stream(t *testing.T) {
	a := integrationProvider(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch, err := a.Stream(ctx, provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Say 'hello' and nothing else."},
		},
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var content strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		content.WriteString(chunk.Content)
	}

	if !strings.Contains(strings.ToLower(content.String()), "hello") {
		t.Errorf("expected streamed content containing 'hello', got %q", content.String())
	}
}

func TestIntegration_HealthCheck(t *testing.T) {
	a := integrationProvider(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := a.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}
}

func integrationProvider(t *testing.T) *Anthropic {
	t.Helper()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping integration test")
	}

	a := &Anthropic{}
	a.config = Config{
		Model:     "claude-sonnet-4-5-20250929",
		MaxTokens: 4096,
		APIKey:    apiKey,
	}
	a.config.defaults()

	ctx := core.NewAppContext(nil, t.TempDir(), t.TempDir())
	if err := a.Provision(ctx); err != nil {
		t.Fatalf("Provision failed: %v", err)
	}
	return a
}
