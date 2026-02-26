package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/provider"
)

func newTestProvider(t *testing.T, handler http.Handler) *Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &Provider{
		config: Config{
			APIKey:  "sk-test",
			Model:   "gpt-4o",
			BaseURL: srv.URL,
		},
		client:        srv.Client(),
		streamClient:  srv.Client(),
		contextWindow: 128000,
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("failed to encode response: %v", err)
	}
}

func readRequestBody(t *testing.T, r *http.Request) chatRequest {
	t.Helper()
	body, _ := io.ReadAll(r.Body)
	var req chatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("invalid request body: %v", err)
	}
	return req
}

func writeSSE(t *testing.T, w http.ResponseWriter, chunks []string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)

	for _, c := range chunks {
		if _, err := w.Write([]byte(c + "\n\n")); err != nil {
			t.Errorf("failed to write SSE chunk: %v", err)
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

func TestComplete_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Error("missing authorization header")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing content-type header")
		}

		req := readRequestBody(t, r)
		if req.Model != "gpt-4o" {
			t.Errorf("model = %q, want gpt-4o", req.Model)
		}
		if req.Stream {
			t.Error("stream should be false for Complete")
		}

		resp := chatResponse{
			Choices: []chatChoice{
				{
					Message:      chatMessage{Role: "assistant", Content: "Hello!"},
					FinishReason: strPtr("stop"),
				},
			},
			Usage: chatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		}
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, resp)
	})

	p := newTestProvider(t, handler)
	resp, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("content = %q, want Hello!", resp.Content)
	}
	if resp.FinishReason != provider.FinishReasonStop {
		t.Errorf("finish_reason = %q, want stop", resp.FinishReason)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("total_tokens = %d, want 15", resp.Usage.TotalTokens)
	}
}

func TestComplete_WithToolCalls(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := chatResponse{
			Choices: []chatChoice{
				{
					Message: chatMessage{
						Role: "assistant",
						ToolCalls: []chatToolCall{
							{
								ID:   "call_1",
								Type: "function",
								Function: chatFunctionCall{
									Name:      "get_weather",
									Arguments: `{"city":"Paris"}`,
								},
							},
						},
					},
					FinishReason: strPtr("tool_calls"),
				},
			},
			Usage: chatUsage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30},
		}
		writeJSON(t, w, resp)
	})

	p := newTestProvider(t, handler)
	resp, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Weather?"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if resp.FinishReason != provider.FinishReasonToolUse {
		t.Errorf("finish_reason = %q, want tool_use", resp.FinishReason)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_weather" {
		t.Errorf("tool name = %q, want get_weather", resp.ToolCalls[0].Name)
	}
}

func TestComplete_WithToolDefinitions(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := readRequestBody(t, r)

		if len(req.Tools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(req.Tools))
		}
		if req.Tools[0].Type != "function" {
			t.Errorf("tool type = %q, want function", req.Tools[0].Type)
		}
		if req.Tools[0].Function.Name != "search" {
			t.Errorf("tool name = %q, want search", req.Tools[0].Function.Name)
		}

		resp := chatResponse{
			Choices: []chatChoice{
				{
					Message:      chatMessage{Role: "assistant", Content: "OK"},
					FinishReason: strPtr("stop"),
				},
			},
		}
		writeJSON(t, w, resp)
	})

	p := newTestProvider(t, handler)
	_, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Search"},
		},
		Tools: []provider.ToolDefinition{
			{
				Name:        "search",
				Description: "Search the web",
				Parameters:  json.RawMessage(`{"type":"object"}`),
			},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
}

func TestComplete_ErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    error
	}{
		{
			name:       "rate_limit",
			statusCode: http.StatusTooManyRequests,
			body:       `{"error":{"message":"Rate limit exceeded"}}`,
			wantErr:    provider.ErrRateLimit,
		},
		{
			name:       "context_length",
			statusCode: http.StatusBadRequest,
			body:       `{"error":{"message":"This model's maximum context_length is 8192 tokens"}}`,
			wantErr:    provider.ErrContextLength,
		},
		{
			name:       "server_error",
			statusCode: http.StatusInternalServerError,
			body:       `{"error":{"message":"Internal server error"}}`,
			wantErr:    provider.ErrProviderDown,
		},
		{
			name:       "auth_error",
			statusCode: http.StatusUnauthorized,
			body:       `{"error":{"message":"Invalid API key"}}`,
			wantErr:    errAuth,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				if _, err := w.Write([]byte(tt.body)); err != nil {
					t.Errorf("failed to write error body: %v", err)
				}
			})

			p := newTestProvider(t, handler)
			_, err := p.Complete(context.Background(), provider.CompletionRequest{
				Messages: []provider.LLMMessage{
					{Role: provider.MessageRoleUser, Content: "Hi"},
				},
			})
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestStream_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := readRequestBody(t, r)

		if !req.Stream {
			t.Error("stream should be true for Stream")
		}
		if req.StreamOptions == nil || !req.StreamOptions.IncludeUsage {
			t.Error("stream_options.include_usage should be true")
		}

		writeSSE(t, w, []string{
			`data: {"choices":[{"delta":{"role":"assistant"},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{"content":" there"},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`,
			`data: [DONE]`,
		})
	})

	p := newTestProvider(t, handler)
	ch, err := p.Stream(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var content strings.Builder
	var gotStop bool
	var lastUsage *provider.TokenUsage
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		content.WriteString(chunk.Content)
		if chunk.FinishReason == provider.FinishReasonStop {
			gotStop = true
		}
		if chunk.Usage != nil {
			lastUsage = chunk.Usage
		}
	}

	if content.String() != "Hello there" {
		t.Errorf("content = %q, want 'Hello there'", content.String())
	}
	if !gotStop {
		t.Error("expected stop finish_reason")
	}
	if lastUsage == nil || lastUsage.TotalTokens != 7 {
		t.Errorf("usage = %v, want total_tokens=7", lastUsage)
	}
}

func TestStream_WithToolCalls(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeSSE(t, w, []string{
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"search","arguments":""}}]},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":"}}]},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"hello\"}"}}]},"finish_reason":null}]}`,
			`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`data: [DONE]`,
		})
	})

	p := newTestProvider(t, handler)
	ch, err := p.Stream(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Search"},
		},
	})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}

	var toolCalls []provider.ToolCall
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		if len(chunk.ToolCalls) > 0 {
			toolCalls = chunk.ToolCalls
		}
	}

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Name != "search" {
		t.Errorf("name = %q, want search", toolCalls[0].Name)
	}
	if string(toolCalls[0].Arguments) != `{"q":"hello"}` {
		t.Errorf("arguments = %s, want {\"q\":\"hello\"}", toolCalls[0].Arguments)
	}
}

func TestStream_HTTPError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		if _, err := w.Write([]byte(`{"error":{"message":"Rate limit exceeded"}}`)); err != nil {
			t.Errorf("failed to write error body: %v", err)
		}
	})

	p := newTestProvider(t, handler)
	_, err := p.Stream(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})
	if !errors.Is(err, provider.ErrRateLimit) {
		t.Errorf("error = %v, want ErrRateLimit", err)
	}
}

func TestComplete_ConfigOverrides(t *testing.T) {
	var receivedReq chatRequest
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedReq = readRequestBody(t, r)

		resp := chatResponse{
			Choices: []chatChoice{
				{
					Message:      chatMessage{Role: "assistant", Content: "OK"},
					FinishReason: strPtr("stop"),
				},
			},
		}
		writeJSON(t, w, resp)
	})

	configTemp := 0.5
	p := newTestProvider(t, handler)
	p.config.Temperature = &configTemp
	p.config.MaxTokens = 1000

	// Request-level override should win.
	reqTemp := 0.9
	_, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
		Temperature: &reqTemp,
		MaxTokens:   500,
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	if receivedReq.Temperature == nil || *receivedReq.Temperature != 0.9 {
		t.Errorf("temperature = %v, want 0.9 (request override)", receivedReq.Temperature)
	}
	if receivedReq.MaxTokens != 500 {
		t.Errorf("max_tokens = %d, want 500 (request override)", receivedReq.MaxTokens)
	}
}

func TestComplete_ConfigDefaults(t *testing.T) {
	var receivedReq chatRequest
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedReq = readRequestBody(t, r)

		resp := chatResponse{
			Choices: []chatChoice{
				{
					Message:      chatMessage{Role: "assistant", Content: "OK"},
					FinishReason: strPtr("stop"),
				},
			},
		}
		writeJSON(t, w, resp)
	})

	configTemp := 0.5
	p := newTestProvider(t, handler)
	p.config.Temperature = &configTemp
	p.config.MaxTokens = 1000

	// No request-level overrides — config defaults should be used.
	_, err := p.Complete(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	if receivedReq.Temperature == nil || *receivedReq.Temperature != 0.5 {
		t.Errorf("temperature = %v, want 0.5 (config default)", receivedReq.Temperature)
	}
	if receivedReq.MaxTokens != 1000 {
		t.Errorf("max_tokens = %d, want 1000 (config default)", receivedReq.MaxTokens)
	}
}

func TestComplete_ContextCancellation(t *testing.T) {
	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second)
	})

	p := newTestProvider(t, handler)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Complete(ctx, provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hi"},
		},
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestHealthCheck(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := readRequestBody(t, r)

		if req.MaxTokens != 1 {
			t.Errorf("health check max_tokens = %d, want 1", req.MaxTokens)
		}

		resp := chatResponse{
			Choices: []chatChoice{
				{
					Message:      chatMessage{Role: "assistant", Content: "."},
					FinishReason: strPtr("stop"),
				},
			},
		}
		writeJSON(t, w, resp)
	})

	p := newTestProvider(t, handler)
	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck() error: %v", err)
	}
}

func TestModelName(t *testing.T) {
	p := &Provider{config: Config{Model: "gpt-4o"}}
	if p.ModelName() != "gpt-4o" {
		t.Errorf("ModelName() = %q, want gpt-4o", p.ModelName())
	}
}

func TestContextWindowSize(t *testing.T) {
	p := &Provider{contextWindow: 128000}
	if p.ContextWindowSize() != 128000 {
		t.Errorf("ContextWindowSize() = %d, want 128000", p.ContextWindowSize())
	}
}
