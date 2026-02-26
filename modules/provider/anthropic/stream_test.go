package anthropic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/provider"
)

func TestStream_TextOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}

		events := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-5-20250929\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":5}}",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}",
		}

		for _, ev := range events {
			_, _ = w.Write([]byte(ev + "\n\n"))
			flusher.Flush()
		}
	}))
	defer srv.Close()

	a := newTestProvider(srv.URL)

	ch, err := a.Stream(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var content strings.Builder
	var gotUsage bool
	var finishReason provider.FinishReason

	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
		if chunk.Content != "" {
			content.WriteString(chunk.Content)
		}
		if chunk.Usage != nil {
			gotUsage = true
		}
		if chunk.FinishReason != "" {
			finishReason = chunk.FinishReason
		}
	}

	if content.String() != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", content.String())
	}
	if !gotUsage {
		t.Error("expected usage chunk")
	}
	if finishReason != provider.FinishReasonStop {
		t.Errorf("expected finish reason 'stop', got %q", finishReason)
	}
}

func TestStream_ToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}

		events := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_2\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-5-20250929\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":15,\"output_tokens\":0}}}",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_01\",\"name\":\"get_weather\"}}",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"city\\\":\"}}",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"\\\"Paris\\\"}\"}}",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":12}}",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}",
		}

		for _, ev := range events {
			_, _ = w.Write([]byte(ev + "\n\n"))
			flusher.Flush()
		}
	}))
	defer srv.Close()

	a := newTestProvider(srv.URL)

	ch, err := a.Stream(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "What's the weather?"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	toolCalls := make([]provider.ToolCall, 0, 1)
	var finishReason provider.FinishReason

	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
		toolCalls = append(toolCalls, chunk.ToolCalls...)
		if chunk.FinishReason != "" {
			finishReason = chunk.FinishReason
		}
	}

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].ID != "toolu_01" {
		t.Errorf("expected tool ID 'toolu_01', got %q", toolCalls[0].ID)
	}
	if toolCalls[0].Name != "get_weather" {
		t.Errorf("expected tool name 'get_weather', got %q", toolCalls[0].Name)
	}
	if string(toolCalls[0].Arguments) != `{"city":"Paris"}` {
		t.Errorf("expected arguments '{\"city\":\"Paris\"}', got %q", string(toolCalls[0].Arguments))
	}
	if finishReason != provider.FinishReasonToolUse {
		t.Errorf("expected finish reason 'tool_use', got %q", finishReason)
	}
}

func TestStream_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}

		// Send first event then hang.
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_3\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-5-20250929\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n"))
		flusher.Flush()

		// Block to simulate a slow server.
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	a := newTestProvider(srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	ch, err := a.Stream(ctx, provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Channel should close within the timeout.
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()

	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // Channel closed — test passes.
			}
		case <-timer.C:
			t.Fatal("stream channel not closed within timeout")
		}
	}
}

func TestStream_InitialError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid api key"}}`))
	}))
	defer srv.Close()

	a := newTestProvider(srv.URL)

	_, err := a.Stream(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hello"},
		},
	})

	// Initial connection error should be returned directly, not via channel.
	if err == nil {
		t.Fatal("expected initial error from Stream, got nil")
	}
}

func TestStream_RateLimitInitial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`))
	}))
	defer srv.Close()

	a := newTestProvider(srv.URL)

	_, err := a.Stream(context.Background(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hello"},
		},
	})

	if err == nil {
		t.Fatal("expected error from Stream, got nil")
	}
	if !errors.Is(err, provider.ErrRateLimit) {
		t.Errorf("expected ErrRateLimit, got %v", err)
	}
}
