//go:build integration

package openai

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/provider"
)

func TestIntegration_Complete(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set, skipping integration test")
	}

	p := &Provider{}
	node := yamlNode(t, "api_key: "+apiKey+"\nmodel: gpt-4o-mini")
	if err := p.Configure(node); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	ctx := core.NewAppContext(nil, t.TempDir(), t.TempDir())
	if err := p.Provision(ctx); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	reqCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := p.Complete(reqCtx, provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Say exactly: hello"},
		},
		MaxTokens: 10,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content == "" {
		t.Error("expected non-empty content")
	}
	t.Logf("Response: %q (tokens: %+v)", resp.Content, resp.Usage)
}

func TestIntegration_Stream(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set, skipping integration test")
	}

	p := &Provider{}
	node := yamlNode(t, "api_key: "+apiKey+"\nmodel: gpt-4o-mini")
	if err := p.Configure(node); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	ctx := core.NewAppContext(nil, t.TempDir(), t.TempDir())
	if err := p.Provision(ctx); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	reqCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch, err := p.Stream(reqCtx, provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Say exactly: hello"},
		},
		MaxTokens: 10,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var content string
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		content += chunk.Content
	}
	if content == "" {
		t.Error("expected non-empty streamed content")
	}
	t.Logf("Streamed: %q", content)
}

func TestIntegration_HealthCheck(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set, skipping integration test")
	}

	p := &Provider{}
	node := yamlNode(t, "api_key: "+apiKey+"\nmodel: gpt-4o-mini")
	if err := p.Configure(node); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	ctx := core.NewAppContext(nil, t.TempDir(), t.TempDir())
	if err := p.Provision(ctx); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	reqCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := p.HealthCheck(reqCtx); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
}
