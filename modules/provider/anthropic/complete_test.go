package anthropic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	sdkanthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/flemzord/sclaw/internal/provider"
)

func TestComplete_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_123",
			"type": "message",
			"role": "assistant",
			"content": [{"type": "text", "text": "Hello!"}],
			"model": "claude-sonnet-4-5-20250929",
			"stop_reason": "end_turn",
			"stop_sequence": null,
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`))
	}))
	defer srv.Close()

	a := newTestProvider(srv.URL)

	resp, err := a.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("expected content 'Hello!', got %q", resp.Content)
	}
	if resp.FinishReason != provider.FinishReasonStop {
		t.Errorf("expected finish reason 'stop', got %q", resp.FinishReason)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("expected total tokens 15, got %d", resp.Usage.TotalTokens)
	}
}

func TestComplete_RateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`))
	}))
	defer srv.Close()

	a := newTestProvider(srv.URL)

	_, err := a.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hello"},
		},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, provider.ErrRateLimit) {
		t.Errorf("expected ErrRateLimit, got %v", err)
	}
}

func TestComplete_ProviderDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}`))
	}))
	defer srv.Close()

	a := newTestProvider(srv.URL)

	_, err := a.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hello"},
		},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, provider.ErrProviderDown) {
		t.Errorf("expected ErrProviderDown, got %v", err)
	}
}

func TestComplete_ContextLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: context length exceeded"}}`))
	}))
	defer srv.Close()

	a := newTestProvider(srv.URL)

	_, err := a.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hello"},
		},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, provider.ErrContextLength) {
		t.Errorf("expected ErrContextLength, got %v", err)
	}
}

func TestComplete_WithToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_456",
			"type": "message",
			"role": "assistant",
			"content": [
				{"type": "text", "text": "Let me check the weather"},
				{"type": "tool_use", "id": "toolu_01", "name": "get_weather", "input": {"city": "Paris"}}
			],
			"model": "claude-sonnet-4-5-20250929",
			"stop_reason": "tool_use",
			"stop_sequence": null,
			"usage": {"input_tokens": 20, "output_tokens": 15}
		}`))
	}))
	defer srv.Close()

	a := newTestProvider(srv.URL)

	resp, err := a.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "What's the weather in Paris?"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Let me check the weather" {
		t.Errorf("expected content, got %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_weather" {
		t.Errorf("expected tool name 'get_weather', got %q", resp.ToolCalls[0].Name)
	}
	if resp.FinishReason != provider.FinishReasonToolUse {
		t.Errorf("expected finish reason 'tool_use', got %q", resp.FinishReason)
	}
}

// newTestProvider creates an Anthropic provider pointed at the given httptest server URL.
func newTestProvider(baseURL string) *Anthropic {
	client := sdkanthropic.NewClient(
		option.WithBaseURL(baseURL),
		option.WithAPIKey("test-key"),
		option.WithMaxRetries(0),
	)
	return &Anthropic{
		config: Config{
			Model:     "claude-sonnet-4-5-20250929",
			MaxTokens: 4096,
		},
		client:        &client,
		contextWindow: 200_000,
	}
}
