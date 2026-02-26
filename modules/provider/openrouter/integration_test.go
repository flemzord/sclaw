package openrouter

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flemzord/sclaw/internal/provider"
)

// writeSSE writes a single SSE data line and flushes if possible.
func writeSSE(w http.ResponseWriter, data string) {
	_, _ = w.Write([]byte("data: " + data + "\n\n"))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// newTestServer creates an httptest.Server with the given handler and
// registers cleanup.
func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// newTestProvider creates an OpenRouter instance pointing at the test server.
func newTestProvider(t *testing.T, srv *httptest.Server) *OpenRouter {
	t.Helper()
	return &OpenRouter{
		config: Config{
			APIKey:  "sk-or-test",
			Model:   "openai/gpt-4o",
			BaseURL: srv.URL,
			Timeout: "10s",
		},
		client: srv.Client(),
	}
}

func TestIntegrationComplete(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req apiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp := apiResponse{
			Choices: []apiChoice{
				{
					Message:      apiMessage{Role: "assistant", Content: "Hello from test!"},
					FinishReason: "stop",
				},
			},
			Usage: apiUsage{PromptTokens: 5, CompletionTokens: 4, TotalTokens: 9},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	o := newTestProvider(t, srv)
	result, err := o.Complete(t.Context(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	if result.Content != "Hello from test!" {
		t.Errorf("Content = %q", result.Content)
	}
	if result.FinishReason != provider.FinishReasonStop {
		t.Errorf("FinishReason = %q", result.FinishReason)
	}
	if result.Usage.TotalTokens != 9 {
		t.Errorf("Usage.TotalTokens = %d", result.Usage.TotalTokens)
	}
}

func TestIntegrationCompleteToolCalling(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		resp := apiResponse{
			Choices: []apiChoice{
				{
					Message: apiMessage{
						Role: "assistant",
						ToolCalls: []apiToolCall{
							{
								ID:   "call_abc",
								Type: "function",
								Function: apiToolCallFn{
									Name:      "get_weather",
									Arguments: `{"city":"Paris"}`,
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
			Usage: apiUsage{PromptTokens: 10, CompletionTokens: 8, TotalTokens: 18},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	o := newTestProvider(t, srv)
	result, err := o.Complete(t.Context(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "What's the weather?"},
		},
		Tools: []provider.ToolDefinition{
			{Name: "get_weather", Description: "Get weather", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	if result.FinishReason != provider.FinishReasonToolUse {
		t.Errorf("FinishReason = %q", result.FinishReason)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("ToolCalls count = %d", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.Name != "get_weather" {
		t.Errorf("ToolCall.Name = %q", tc.Name)
	}
	if tc.ID != "call_abc" {
		t.Errorf("ToolCall.ID = %q", tc.ID)
	}
}

func TestIntegrationCompleteRateLimit(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limit exceeded"}}`))
	})

	o := newTestProvider(t, srv)
	_, err := o.Complete(t.Context(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, provider.ErrRateLimit) {
		t.Errorf("error = %v, want ErrRateLimit", err)
	}
}

func TestIntegrationCompleteServerError(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"bad gateway"}}`))
	})

	o := newTestProvider(t, srv)
	_, err := o.Complete(t.Context(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, provider.ErrProviderDown) {
		t.Errorf("error = %v, want ErrProviderDown", err)
	}
}

func TestIntegrationCompleteContextLength(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"context length exceeded"}}`))
	})

	o := newTestProvider(t, srv)
	_, err := o.Complete(t.Context(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, provider.ErrContextLength) {
		t.Errorf("error = %v, want ErrContextLength", err)
	}
}

func TestIntegrationCompleteAuthError(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	})

	o := newTestProvider(t, srv)
	_, err := o.Complete(t.Context(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})

	if err == nil {
		t.Fatal("expected error")
	}
	// Auth errors are NOT retryable (no sentinel wrapping).
	if errors.Is(err, provider.ErrRateLimit) || errors.Is(err, provider.ErrProviderDown) {
		t.Errorf("auth error should not be retryable: %v", err)
	}
}

func TestIntegrationCompleteBadRequest(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid model"}}`))
	})

	o := newTestProvider(t, srv)
	_, err := o.Complete(t.Context(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})

	if err == nil {
		t.Fatal("expected error")
	}
	// Generic 400 (not context length) is NOT retryable.
	if errors.Is(err, provider.ErrRateLimit) || errors.Is(err, provider.ErrProviderDown) || errors.Is(err, provider.ErrContextLength) {
		t.Errorf("generic 400 should not wrap a sentinel: %v", err)
	}
}

func TestIntegrationStream(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		writeSSE(w, `{"choices":[{"delta":{"content":"Hello"},"finish_reason":""}]}`)
		writeSSE(w, `{"choices":[{"delta":{"content":" world"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
		writeSSE(w, "[DONE]")
	})

	o := newTestProvider(t, srv)
	ch, err := o.Stream(t.Context(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var content string
	var lastChunk provider.StreamChunk
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		content += chunk.Content
		lastChunk = chunk
	}

	if content != "Hello world" {
		t.Errorf("Content = %q, want %q", content, "Hello world")
	}
	if lastChunk.FinishReason != provider.FinishReasonStop {
		t.Errorf("FinishReason = %q", lastChunk.FinishReason)
	}
	if lastChunk.Usage == nil {
		t.Fatal("expected usage on last chunk")
	}
	if lastChunk.Usage.TotalTokens != 5 {
		t.Errorf("Usage.TotalTokens = %d", lastChunk.Usage.TotalTokens)
	}
}

func TestIntegrationStreamRateLimit(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limit"}}`))
	})

	o := newTestProvider(t, srv)
	_, err := o.Stream(t.Context(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, provider.ErrRateLimit) {
		t.Errorf("error = %v, want ErrRateLimit", err)
	}
}

func TestIntegrationStreamToolCalls(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		writeSSE(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"search","arguments":""}}]},"finish_reason":""}]}`)
		writeSSE(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":"}}]},"finish_reason":""}]}`)
		writeSSE(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"test\"}"}}]},"finish_reason":"tool_calls"}]}`)
		writeSSE(w, "[DONE]")
	})

	o := newTestProvider(t, srv)
	ch, err := o.Stream(t.Context(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Search for test"},
		},
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var lastChunk provider.StreamChunk
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		lastChunk = chunk
	}

	if lastChunk.FinishReason != provider.FinishReasonToolUse {
		t.Errorf("FinishReason = %q", lastChunk.FinishReason)
	}
	if len(lastChunk.ToolCalls) != 1 {
		t.Fatalf("ToolCalls count = %d", len(lastChunk.ToolCalls))
	}
	tc := lastChunk.ToolCalls[0]
	if tc.ID != "call_1" {
		t.Errorf("ToolCall.ID = %q", tc.ID)
	}
	if tc.Name != "search" {
		t.Errorf("ToolCall.Name = %q", tc.Name)
	}
	if string(tc.Arguments) != `{"q":"test"}` {
		t.Errorf("ToolCall.Arguments = %s", tc.Arguments)
	}
}

func TestIntegrationStreamWithKeepalive(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		writeSSE(w, ": OPENROUTER PROCESSING")
		writeSSE(w, ": OPENROUTER PROCESSING")
		writeSSE(w, `{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
		writeSSE(w, "[DONE]")
	})

	o := newTestProvider(t, srv)
	ch, err := o.Stream(t.Context(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var content string
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		content += chunk.Content
	}

	if content != "ok" {
		t.Errorf("Content = %q, want %q", content, "ok")
	}
}

func TestIntegrationAutoModel(t *testing.T) {
	t.Parallel()

	var receivedModel string
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req apiRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		receivedModel = req.Model

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiResponse{
			Choices: []apiChoice{{Message: apiMessage{Content: "auto"}, FinishReason: "stop"}},
		})
	})

	o := &OpenRouter{
		config: Config{
			APIKey:  "sk-or-test",
			Model:   "auto",
			BaseURL: srv.URL,
		},
		client: srv.Client(),
	}

	_, err := o.Complete(t.Context(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	if receivedModel != "openrouter/auto" {
		t.Errorf("model sent = %q, want %q", receivedModel, "openrouter/auto")
	}
}

func TestIntegrationHealthCheck(t *testing.T) {
	t.Parallel()

	var called bool
	srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiResponse{
			Choices: []apiChoice{{Message: apiMessage{Content: "p"}, FinishReason: "stop"}},
			Usage:   apiUsage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
		})
	})

	o := newTestProvider(t, srv)
	if err := o.HealthCheck(t.Context()); err != nil {
		t.Fatalf("HealthCheck() error: %v", err)
	}
	if !called {
		t.Error("health check did not send a request")
	}
}

func TestIntegrationCustomHeaders(t *testing.T) {
	t.Parallel()

	var headers http.Header
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiResponse{
			Choices: []apiChoice{{Message: apiMessage{Content: "ok"}, FinishReason: "stop"}},
		})
	})

	o := &OpenRouter{
		config: Config{
			APIKey:  "sk-or-test",
			Model:   "test",
			BaseURL: srv.URL,
			Referer: "https://example.com",
			Title:   "MyBot",
		},
		client: srv.Client(),
	}

	_, err := o.Complete(t.Context(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	if got := headers.Get("HTTP-Referer"); got != "https://example.com" {
		t.Errorf("HTTP-Referer = %q", got)
	}
	if got := headers.Get("X-Title"); got != "MyBot" {
		t.Errorf("X-Title = %q", got)
	}
}

func TestIntegrationNoOptionalHeaders(t *testing.T) {
	t.Parallel()

	var headers http.Header
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiResponse{
			Choices: []apiChoice{{Message: apiMessage{Content: "ok"}, FinishReason: "stop"}},
		})
	})

	o := newTestProvider(t, srv)
	_, err := o.Complete(t.Context(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{{Role: provider.MessageRoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	if got := headers.Get("HTTP-Referer"); got != "" {
		t.Errorf("HTTP-Referer should be empty, got %q", got)
	}
	if got := headers.Get("X-Title"); got != "" {
		t.Errorf("X-Title should be empty, got %q", got)
	}
}
