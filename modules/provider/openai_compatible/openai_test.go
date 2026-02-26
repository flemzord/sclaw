package openaicompat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/provider"
	"gopkg.in/yaml.v3"
)

func newTestProvider(baseURL string) *Provider {
	return &Provider{
		config: Config{
			BaseURL:       baseURL,
			APIKey:        "test-key",
			Model:         "test-model",
			ContextWindow: 4096,
			Timeout:       5 * time.Second,
		},
		client: &http.Client{
			Transport: &http.Transport{
				ResponseHeaderTimeout: 5 * time.Second,
			},
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestConfigure(t *testing.T) {
	yamlData := `
base_url: "https://api.example.com/v1"
api_key: "sk-test-123"
model: "gpt-4"
context_window: 8192
max_tokens: 1024
headers:
  X-Custom: "value"
timeout: 60s
`
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(yamlData), &node); err != nil {
		t.Fatalf("unmarshal yaml: %v", err)
	}

	p := &Provider{}
	if err := p.Configure(node.Content[0]); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	if p.config.BaseURL != "https://api.example.com/v1" {
		t.Errorf("BaseURL = %q, want %q", p.config.BaseURL, "https://api.example.com/v1")
	}
	if p.config.APIKey != "sk-test-123" {
		t.Errorf("APIKey = %q, want %q", p.config.APIKey, "sk-test-123")
	}
	if p.config.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", p.config.Model, "gpt-4")
	}
	if p.config.ContextWindow != 8192 {
		t.Errorf("ContextWindow = %d, want %d", p.config.ContextWindow, 8192)
	}
	if p.config.MaxTokens != 1024 {
		t.Errorf("MaxTokens = %d, want %d", p.config.MaxTokens, 1024)
	}
	if p.config.Timeout != 60*time.Second {
		t.Errorf("Timeout = %v, want %v", p.config.Timeout, 60*time.Second)
	}
	if v := p.config.Headers["X-Custom"]; v != "value" {
		t.Errorf("Headers[X-Custom] = %q, want %q", v, "value")
	}
}

func TestConfigure_Defaults(t *testing.T) {
	yamlData := `
base_url: "https://api.example.com/v1"
api_key: "sk-test"
model: "gpt-4"
`
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(yamlData), &node); err != nil {
		t.Fatalf("unmarshal yaml: %v", err)
	}

	p := &Provider{}
	if err := p.Configure(node.Content[0]); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	if p.config.Timeout != 30*time.Second {
		t.Errorf("default Timeout = %v, want %v", p.config.Timeout, 30*time.Second)
	}
	if p.config.ContextWindow != 4096 {
		t.Errorf("default ContextWindow = %d, want %d", p.config.ContextWindow, 4096)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr string
	}{
		{
			name:    "missing base_url",
			config:  Config{APIKey: "k", Model: "m"},
			wantErr: "base_url",
		},
		{
			name:    "missing api_key",
			config:  Config{BaseURL: "http://localhost", Model: "m"},
			wantErr: "api_key",
		},
		{
			name:    "missing model",
			config:  Config{BaseURL: "http://localhost", APIKey: "k"},
			wantErr: "model",
		},
		{
			name:   "valid",
			config: Config{BaseURL: "http://localhost", APIKey: "k", Model: "m"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Provider{config: tt.config}
			err := p.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("path = %s, want /chat/completions", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer test-key")
		}

		writeJSON(w, oaiResponse{
			Choices: []oaiChoice{
				{
					Message:      oaiMessage{Role: "assistant", Content: "Hello!"},
					FinishReason: "stop",
				},
			},
			Usage: oaiUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		})
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)

	resp, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if resp.Content != "Hello!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello!")
	}
	if resp.FinishReason != provider.FinishReasonStop {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, provider.FinishReasonStop)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want %d", resp.Usage.TotalTokens, 15)
	}
}

func TestComplete_ToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, oaiResponse{
			Choices: []oaiChoice{
				{
					Message: oaiMessage{
						Role: "assistant",
						ToolCalls: []oaiToolCall{
							{
								ID:   "call_123",
								Type: "function",
								Function: oaiToolFunction{
									Name:      "get_weather",
									Arguments: `{"location":"Paris"}`,
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
			Usage: oaiUsage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30},
		})
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)

	resp, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Weather in Paris?"},
		},
		Tools: []provider.ToolDefinition{
			{
				Name:        "get_weather",
				Description: "Get the weather",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`),
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if resp.FinishReason != provider.FinishReasonToolUse {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, provider.FinishReasonToolUse)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_123" {
		t.Errorf("ToolCall.ID = %q, want %q", tc.ID, "call_123")
	}
	if tc.Name != "get_weather" {
		t.Errorf("ToolCall.Name = %q, want %q", tc.Name, "get_weather")
	}
}

func TestComplete_RateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, "rate limited")
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "Hi"}},
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !provider.IsRateLimit(err) {
		t.Errorf("expected ErrRateLimit, got: %v", err)
	}
}

func TestComplete_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "internal error")
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "Hi"}},
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !provider.IsRetryable(err) {
		t.Errorf("expected retryable error (ErrProviderDown), got: %v", err)
	}
}

func TestStream(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"Hel"},"finish_reason":null}]}

data: {"choices":[{"delta":{"content":"lo!"},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}

data: [DONE]

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, sseData)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)

	ch, err := p.Stream(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var contents []string
	var gotFinish bool
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
		if chunk.Content != "" {
			contents = append(contents, chunk.Content)
		}
		if chunk.FinishReason == provider.FinishReasonStop {
			gotFinish = true
		}
	}

	joined := strings.Join(contents, "")
	if joined != "Hello!" {
		t.Errorf("streamed content = %q, want %q", joined, "Hello!")
	}
	if !gotFinish {
		t.Error("expected stop finish reason")
	}
}

func TestStream_ToolCallDeltas(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"search","arguments":""}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"test\"}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, sseData)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)

	ch, err := p.Stream(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "search"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var toolCalls []provider.ToolCall
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
		if len(chunk.ToolCalls) > 0 {
			toolCalls = chunk.ToolCalls
		}
	}

	if len(toolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(toolCalls))
	}
	if toolCalls[0].ID != "call_1" {
		t.Errorf("ToolCall.ID = %q, want %q", toolCalls[0].ID, "call_1")
	}
	if toolCalls[0].Name != "search" {
		t.Errorf("ToolCall.Name = %q, want %q", toolCalls[0].Name, "search")
	}
	if string(toolCalls[0].Arguments) != `{"q":"test"}` {
		t.Errorf("ToolCall.Arguments = %s, want %s", toolCalls[0].Arguments, `{"q":"test"}`)
	}
}

func TestStream_MidStreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Write partial data then close connection abruptly.
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"},\"finish_reason\":null}]}\n\n")
		// Flush then close to simulate mid-stream break.
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)

	ch, err := p.Stream(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var gotContent bool
	for chunk := range ch {
		if chunk.Content != "" {
			gotContent = true
		}
		// We may or may not get an error depending on how the connection closes.
		// The important thing is the channel eventually closes.
	}

	if !gotContent {
		t.Error("expected at least one content chunk before stream ended")
	}
}

func TestStream_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Block until client disconnects.
		<-r.Context().Done()
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)

	ctx, cancel := context.WithCancel(context.Background())

	ch, err := p.Stream(ctx, provider.CompletionRequest{
		Messages: []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// Cancel context after a short delay.
	cancel()

	// The channel should eventually close.
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // channel closed — success
			}
		case <-timer.C:
			t.Fatal("stream channel did not close after context cancellation")
		}
	}
}

func TestHealthCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/models") {
			t.Errorf("path = %s, want /models", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"data":[]}`)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)

	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
}

func TestHealthCheck_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)

	err := p.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCustomHeaders(t *testing.T) {
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		writeJSON(w, oaiResponse{
			Choices: []oaiChoice{
				{Message: oaiMessage{Role: "assistant", Content: "ok"}, FinishReason: "stop"},
			},
		})
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	p.config.Headers = map[string]string{
		"X-Custom-Header": "custom-value",
		"X-Another":       "another-value",
	}

	_, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if v := gotHeaders.Get("X-Custom-Header"); v != "custom-value" {
		t.Errorf("X-Custom-Header = %q, want %q", v, "custom-value")
	}
	if v := gotHeaders.Get("X-Another"); v != "another-value" {
		t.Errorf("X-Another = %q, want %q", v, "another-value")
	}
}

func TestModelName(t *testing.T) {
	p := &Provider{config: Config{Model: "gpt-4-turbo"}}
	if got := p.ModelName(); got != "gpt-4-turbo" {
		t.Errorf("ModelName() = %q, want %q", got, "gpt-4-turbo")
	}
}

func TestContextWindowSize(t *testing.T) {
	p := &Provider{config: Config{ContextWindow: 128000}}
	if got := p.ContextWindowSize(); got != 128000 {
		t.Errorf("ContextWindowSize() = %d, want %d", got, 128000)
	}
}

func TestStream_SSEWithoutSpace(t *testing.T) {
	// Some providers send "data:{json}" without a space after the colon.
	sseData := "data:{\"choices\":[{\"delta\":{\"content\":\"Hi\"},\"finish_reason\":null}]}\n\ndata:[DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, sseData)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)

	ch, err := p.Stream(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var gotContent bool
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
		if chunk.Content == "Hi" {
			gotContent = true
		}
	}

	if !gotContent {
		t.Error("expected content 'Hi' from SSE without space after data:")
	}
}

func TestConfigMaxTokensFallback(t *testing.T) {
	var gotBody oaiRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeJSON(w, oaiResponse{
			Choices: []oaiChoice{
				{Message: oaiMessage{Role: "assistant", Content: "ok"}, FinishReason: "stop"},
			},
		})
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	p.config.MaxTokens = 2048

	// Request with no MaxTokens should fall back to config value.
	_, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if gotBody.MaxTokens != 2048 {
		t.Errorf("max_tokens = %d, want 2048 (config fallback)", gotBody.MaxTokens)
	}

	// Request with explicit MaxTokens should override config.
	_, err = p.Complete(context.Background(), provider.CompletionRequest{
		Messages:  []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "Hi"}},
		MaxTokens: 512,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if gotBody.MaxTokens != 512 {
		t.Errorf("max_tokens = %d, want 512 (explicit override)", gotBody.MaxTokens)
	}
}

func TestComplete_ContextCancelNotProviderDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// Block until client disconnects.
		<-r.Context().Done()
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := p.Complete(ctx, provider.CompletionRequest{
		Messages: []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "Hi"}},
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Should be a context error, NOT classified as ErrProviderDown.
	if provider.IsRetryable(err) {
		t.Errorf("context cancel should not be retryable (ErrProviderDown), got: %v", err)
	}
}

func TestComplete_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":"invalid api key"}`)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "Hi"}},
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("expected authentication error, got: %v", err)
	}
	// Auth errors should NOT be retryable.
	if provider.IsRetryable(err) {
		t.Errorf("auth error should not be retryable, got: %v", err)
	}
}

func TestComplete_Forbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, "access denied")
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "Hi"}},
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("expected authentication error, got: %v", err)
	}
}

func TestComplete_ContextLengthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"error":{"message":"This model's maximum context length is 4096 tokens","type":"invalid_request_error","code":"context_length_exceeded"}}`)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "Hi"}},
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "context length exceeded") {
		t.Errorf("expected context length error, got: %v", err)
	}
	// Context length errors should NOT be retryable.
	if provider.IsRetryable(err) {
		t.Errorf("context length error should not be retryable, got: %v", err)
	}
}

func TestComplete_EmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, oaiResponse{
			Choices: []oaiChoice{},
			Usage:   oaiUsage{PromptTokens: 5, CompletionTokens: 0, TotalTokens: 5},
		})
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	resp, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if resp.Content != "" {
		t.Errorf("Content = %q, want empty", resp.Content)
	}
	if resp.Usage.TotalTokens != 5 {
		t.Errorf("TotalTokens = %d, want 5", resp.Usage.TotalTokens)
	}
}

func TestComplete_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{invalid json`)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "Hi"}},
	})

	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Errorf("expected decode error, got: %v", err)
	}
}

func TestStream_WithoutDone(t *testing.T) {
	// Simulates a provider that closes the connection without sending [DONE].
	// Tool calls should still be emitted (fix for C-02).
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_x","type":"function","function":{"name":"lookup","arguments":""}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"id\":1}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, sseData)
		// Connection closes without [DONE].
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)

	ch, err := p.Stream(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "lookup"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var toolCalls []provider.ToolCall
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
		if len(chunk.ToolCalls) > 0 {
			toolCalls = chunk.ToolCalls
		}
	}

	if len(toolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(toolCalls))
	}
	if toolCalls[0].ID != "call_x" {
		t.Errorf("ToolCall.ID = %q, want %q", toolCalls[0].ID, "call_x")
	}
	if toolCalls[0].Name != "lookup" {
		t.Errorf("ToolCall.Name = %q, want %q", toolCalls[0].Name, "lookup")
	}
	if string(toolCalls[0].Arguments) != `{"id":1}` {
		t.Errorf("ToolCall.Arguments = %s, want %s", toolCalls[0].Arguments, `{"id":1}`)
	}
}

func TestStream_MultipleToolCalls(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"search","arguments":"{\"q\":\"a\"}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"fetch","arguments":"{\"url\":\"b\"}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, sseData)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)

	ch, err := p.Stream(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "multi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var toolCalls []provider.ToolCall
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
		if len(chunk.ToolCalls) > 0 {
			toolCalls = chunk.ToolCalls
		}
	}

	if len(toolCalls) != 2 {
		t.Fatalf("ToolCalls len = %d, want 2", len(toolCalls))
	}
	if toolCalls[0].Name != "search" {
		t.Errorf("ToolCall[0].Name = %q, want %q", toolCalls[0].Name, "search")
	}
	if toolCalls[1].Name != "fetch" {
		t.Errorf("ToolCall[1].Name = %q, want %q", toolCalls[1].Name, "fetch")
	}
}

func TestValidate_NegativeContextWindow(t *testing.T) {
	p := &Provider{config: Config{
		BaseURL:       "http://localhost",
		APIKey:        "k",
		Model:         "m",
		ContextWindow: -1,
	}}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for negative context_window")
	}
	if !strings.Contains(err.Error(), "context_window") {
		t.Errorf("error should mention context_window: %v", err)
	}
}

func TestValidate_NegativeMaxTokens(t *testing.T) {
	p := &Provider{config: Config{
		BaseURL:   "http://localhost",
		APIKey:    "k",
		Model:     "m",
		MaxTokens: -100,
	}}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for negative max_tokens")
	}
	if !strings.Contains(err.Error(), "max_tokens") {
		t.Errorf("error should mention max_tokens: %v", err)
	}
}

func TestValidate_InvalidBaseURLScheme(t *testing.T) {
	p := &Provider{config: Config{
		BaseURL: "ftp://example.com",
		APIKey:  "k",
		Model:   "m",
	}}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error for ftp scheme")
	}
	if !strings.Contains(err.Error(), "scheme") {
		t.Errorf("error should mention scheme: %v", err)
	}
}

func TestValidate_APIKeyEnv(t *testing.T) {
	// api_key_env alone should be valid (api_key can be empty).
	p := &Provider{config: Config{
		BaseURL:   "http://localhost",
		APIKeyEnv: "MY_API_KEY",
		Model:     "m",
	}}
	if err := p.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_NeitherAPIKeyNorEnv(t *testing.T) {
	p := &Provider{config: Config{
		BaseURL: "http://localhost",
		Model:   "m",
	}}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected error when neither api_key nor api_key_env is set")
	}
	if !strings.Contains(err.Error(), "api_key") {
		t.Errorf("error should mention api_key: %v", err)
	}
}

func TestProvision_APIKeyEnv(t *testing.T) {
	t.Setenv("TEST_OPENAI_KEY", "sk-from-env")

	p := &Provider{config: Config{
		BaseURL:   "http://localhost",
		APIKeyEnv: "TEST_OPENAI_KEY",
		Model:     "m",
		Timeout:   5 * time.Second,
	}}
	p.config.defaults()

	ctx := &core.AppContext{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	if err := p.Provision(ctx); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if p.config.APIKey != "sk-from-env" {
		t.Errorf("APIKey = %q, want %q", p.config.APIKey, "sk-from-env")
	}
}

func TestProvision_APIKeyEnvMissing(t *testing.T) {
	p := &Provider{config: Config{
		BaseURL:   "http://localhost",
		APIKeyEnv: "NONEXISTENT_KEY_FOR_TEST",
		Model:     "m",
		Timeout:   5 * time.Second,
	}}
	p.config.defaults()

	ctx := &core.AppContext{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	err := p.Provision(ctx)
	if err == nil {
		t.Fatal("expected error for missing env var")
	}
	if !strings.Contains(err.Error(), "NONEXISTENT_KEY_FOR_TEST") {
		t.Errorf("error should mention env var name: %v", err)
	}
}

func TestStream_StreamOptionsIncludeUsage(t *testing.T) {
	var gotBody oaiRequest
	sseData := "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, sseData)
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	ch, err := p.Stream(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// Drain channel.
	for chunk := range ch {
		_ = chunk
	}

	if !gotBody.Stream {
		t.Error("expected stream=true in request body")
	}
	if gotBody.StreamOptions == nil {
		t.Fatal("expected stream_options in request body")
	}
	if !gotBody.StreamOptions.IncludeUsage {
		t.Error("expected stream_options.include_usage=true")
	}
}

func TestMapFinishReason_Unknown(t *testing.T) {
	got := mapFinishReason("something_new")
	if got != provider.FinishReason("something_new") {
		t.Errorf("mapFinishReason(\"something_new\") = %q, want %q", got, "something_new")
	}
}

func TestBaseURL_TrailingSlashNormalized(t *testing.T) {
	c := Config{BaseURL: "https://api.example.com/v1/"}
	c.defaults()
	if c.BaseURL != "https://api.example.com/v1" {
		t.Errorf("BaseURL = %q, want trailing slash removed", c.BaseURL)
	}
}
