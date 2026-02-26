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
			config:  Config{BaseURL: "u", Model: "m"},
			wantErr: "api_key",
		},
		{
			name:    "missing model",
			config:  Config{BaseURL: "u", APIKey: "k"},
			wantErr: "model",
		},
		{
			name:   "valid",
			config: Config{BaseURL: "u", APIKey: "k", Model: "m"},
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
