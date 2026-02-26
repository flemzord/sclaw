package openai

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/flemzord/sclaw/internal/provider"
)

func TestReadStream_BasicContent(t *testing.T) {
	data := `data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"choices":[{"delta":{"content":" world"},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	ch := make(chan provider.StreamChunk, 64)
	go readStream(context.Background(), io.NopCloser(strings.NewReader(data)), ch)

	var content strings.Builder
	var gotStop bool
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
		content.WriteString(chunk.Content)
		if chunk.FinishReason == provider.FinishReasonStop {
			gotStop = true
		}
	}

	if content.String() != "Hello world" {
		t.Errorf("content = %q, want 'Hello world'", content.String())
	}
	if !gotStop {
		t.Error("expected stop finish_reason")
	}
}

func TestReadStream_DONE_Terminal(t *testing.T) {
	data := `data: {"choices":[{"delta":{"content":"Hi"},"finish_reason":null}]}

data: [DONE]

`
	ch := make(chan provider.StreamChunk, 64)
	go readStream(context.Background(), io.NopCloser(strings.NewReader(data)), ch)

	count := 0
	for range ch {
		count++
	}
	if count != 1 {
		t.Errorf("expected 1 chunk, got %d", count)
	}
}

func TestReadStream_CommentsIgnored(t *testing.T) {
	data := `: this is a comment
data: {"choices":[{"delta":{"content":"ok"},"finish_reason":null}]}

data: [DONE]

`
	ch := make(chan provider.StreamChunk, 64)
	go readStream(context.Background(), io.NopCloser(strings.NewReader(data)), ch)

	var content string
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
		content += chunk.Content
	}
	if content != "ok" {
		t.Errorf("content = %q, want 'ok'", content)
	}
}

func TestReadStream_MalformedJSON(t *testing.T) {
	data := `data: {not json}

`
	ch := make(chan provider.StreamChunk, 64)
	go readStream(context.Background(), io.NopCloser(strings.NewReader(data)), ch)

	chunk := <-ch
	if chunk.Err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestReadStream_ToolCallAccumulation(t *testing.T) {
	data := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"search","arguments":""}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"test\"}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	ch := make(chan provider.StreamChunk, 64)
	go readStream(context.Background(), io.NopCloser(strings.NewReader(data)), ch)

	var toolCalls []provider.ToolCall
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
		if len(chunk.ToolCalls) > 0 {
			toolCalls = chunk.ToolCalls
		}
	}

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].ID != "call_1" {
		t.Errorf("id = %q, want call_1", toolCalls[0].ID)
	}
	if toolCalls[0].Name != "search" {
		t.Errorf("name = %q, want search", toolCalls[0].Name)
	}
	if string(toolCalls[0].Arguments) != `{"q":"test"}` {
		t.Errorf("arguments = %s, want {\"q\":\"test\"}", toolCalls[0].Arguments)
	}
}

func TestReadStream_MultipleToolCalls(t *testing.T) {
	data := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"search","arguments":"{\"q\":\"a\"}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"lookup","arguments":"{\"id\":1}"}}]},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	ch := make(chan provider.StreamChunk, 64)
	go readStream(context.Background(), io.NopCloser(strings.NewReader(data)), ch)

	var toolCalls []provider.ToolCall
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
		if len(chunk.ToolCalls) > 0 {
			toolCalls = chunk.ToolCalls
		}
	}

	if len(toolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(toolCalls))
	}
	// Verify sorted by index.
	if toolCalls[0].Name != "search" || toolCalls[1].Name != "lookup" {
		t.Errorf("tool calls not in order: %v", toolCalls)
	}
}

func TestReadStream_ContextCancellation(t *testing.T) {
	// Simulate a stream that blocks forever.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()

	ch := make(chan provider.StreamChunk, 64)
	go readStream(ctx, pr, ch)

	// The goroutine should exit promptly. It may send a context.Canceled
	// error chunk, or it may simply close the channel if the context-aware
	// send picked the ctx.Done() branch. Both are correct cancellation behavior.
	for chunk := range ch {
		if chunk.Err != nil && !errors.Is(chunk.Err, context.Canceled) {
			t.Errorf("unexpected error: %v", chunk.Err)
		}
	}
	// Channel closed — goroutine exited without hanging.
}

func TestReadStream_UsageChunk(t *testing.T) {
	data := `data: {"choices":[{"delta":{"content":"Hi"},"finish_reason":null}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}

data: [DONE]

`
	ch := make(chan provider.StreamChunk, 64)
	go readStream(context.Background(), io.NopCloser(strings.NewReader(data)), ch)

	var lastUsage *provider.TokenUsage
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
		if chunk.Usage != nil {
			lastUsage = chunk.Usage
		}
	}

	if lastUsage == nil {
		t.Fatal("expected usage in stream")
	}
	if lastUsage.TotalTokens != 6 {
		t.Errorf("total_tokens = %d, want 6", lastUsage.TotalTokens)
	}
}
