package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/tool"
)

// mockProvider returns pre-configured responses in sequence.
type mockProvider struct {
	mu        sync.Mutex
	responses []provider.CompletionResponse
	streams   [][]provider.StreamChunk
	callIdx   int
	streamIdx int
}

func (m *mockProvider) Complete(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.callIdx >= len(m.responses) {
		return provider.CompletionResponse{}, fmt.Errorf("no more mock responses")
	}
	resp := m.responses[m.callIdx]
	m.callIdx++
	return resp, nil
}

func (m *mockProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.streamIdx >= len(m.streams) {
		return nil, fmt.Errorf("no more mock streams")
	}
	chunks := m.streams[m.streamIdx]
	m.streamIdx++
	ch := make(chan provider.StreamChunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (m *mockProvider) ContextWindowSize() int { return 128000 }
func (m *mockProvider) ModelName() string      { return "mock-model" }

func newLoopTestExecutor(tools ...*mockTool) *ToolExecutor {
	reg := tool.NewRegistry()
	for _, t := range tools {
		if err := reg.Register(t); err != nil {
			panic(err)
		}
	}
	return newTestExecutor(reg)
}

func newTestLoop(p provider.Provider, executor *ToolExecutor, cfg LoopConfig) *Loop {
	return NewLoop(p, executor, cfg)
}

func userMsg(content string) provider.LLMMessage {
	return provider.LLMMessage{Role: provider.MessageRoleUser, Content: content}
}

// TestRun_TextResponse: provider returns text, no tool calls → StopReasonComplete.
func TestRun_TextResponse(t *testing.T) {
	t.Parallel()

	p := &mockProvider{
		responses: []provider.CompletionResponse{
			{Content: "hello world", FinishReason: provider.FinishReasonStop},
		},
	}
	executor := newLoopTestExecutor()
	loop := newTestLoop(p, executor, LoopConfig{MaxIterations: 5})

	resp, err := loop.Run(context.Background(), Request{
		Messages: []provider.LLMMessage{userMsg("hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StopReason != StopReasonComplete {
		t.Errorf("expected StopReasonComplete, got %s", resp.StopReason)
	}
	if resp.Content != "hello world" {
		t.Errorf("expected content 'hello world', got %q", resp.Content)
	}
	if resp.Iterations != 1 {
		t.Errorf("expected 1 iteration, got %d", resp.Iterations)
	}
}

// TestRun_ToolExecution: provider calls tool → result reinjected → provider returns text.
func TestRun_ToolExecution(t *testing.T) {
	t.Parallel()

	readTool := &mockTool{
		name:   "read",
		output: tool.Output{Content: "file content"},
	}
	p := &mockProvider{
		responses: []provider.CompletionResponse{
			{
				ToolCalls:    []provider.ToolCall{{ID: "1", Name: "read", Arguments: json.RawMessage(`{}`)}},
				FinishReason: provider.FinishReasonToolUse,
				Usage:        provider.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			},
			{
				Content:      "done",
				FinishReason: provider.FinishReasonStop,
				Usage:        provider.TokenUsage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30},
			},
		},
	}
	executor := newLoopTestExecutor(readTool)
	loop := newTestLoop(p, executor, LoopConfig{MaxIterations: 5})

	resp, err := loop.Run(context.Background(), Request{
		Messages: []provider.LLMMessage{userMsg("read a file")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StopReason != StopReasonComplete {
		t.Errorf("expected StopReasonComplete, got %s", resp.StopReason)
	}
	if resp.Iterations != 2 {
		t.Errorf("expected 2 iterations, got %d", resp.Iterations)
	}
	if len(resp.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.TotalUsage.TotalTokens != 45 {
		t.Errorf("expected total tokens 45, got %d", resp.TotalUsage.TotalTokens)
	}
}

// TestRun_ParallelToolCalls: provider requests 3 tools at once → all results reinjected.
func TestRun_ParallelToolCalls(t *testing.T) {
	t.Parallel()

	tool1 := &mockTool{name: "tool1", output: tool.Output{Content: "result1"}}
	tool2 := &mockTool{name: "tool2", output: tool.Output{Content: "result2"}}
	tool3 := &mockTool{name: "tool3", output: tool.Output{Content: "result3"}}

	p := &mockProvider{
		responses: []provider.CompletionResponse{
			{
				ToolCalls: []provider.ToolCall{
					{ID: "1", Name: "tool1", Arguments: json.RawMessage(`{}`)},
					{ID: "2", Name: "tool2", Arguments: json.RawMessage(`{}`)},
					{ID: "3", Name: "tool3", Arguments: json.RawMessage(`{}`)},
				},
				FinishReason: provider.FinishReasonToolUse,
			},
			{Content: "all done", FinishReason: provider.FinishReasonStop},
		},
	}
	executor := newLoopTestExecutor(tool1, tool2, tool3)
	loop := newTestLoop(p, executor, LoopConfig{MaxIterations: 5})

	resp, err := loop.Run(context.Background(), Request{
		Messages: []provider.LLMMessage{userMsg("run all tools")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StopReason != StopReasonComplete {
		t.Errorf("expected StopReasonComplete, got %s", resp.StopReason)
	}
	if len(resp.ToolCalls) != 3 {
		t.Errorf("expected 3 tool calls, got %d", len(resp.ToolCalls))
	}
}

// TestRun_ToolError: tool returns IsError output → LLM gets error, continues.
func TestRun_ToolError(t *testing.T) {
	t.Parallel()

	errTool := &mockTool{
		name:   "failing_tool",
		output: tool.Output{Content: "something went wrong", IsError: true},
	}
	p := &mockProvider{
		responses: []provider.CompletionResponse{
			{
				ToolCalls:    []provider.ToolCall{{ID: "1", Name: "failing_tool", Arguments: json.RawMessage(`{}`)}},
				FinishReason: provider.FinishReasonToolUse,
			},
			{Content: "I see the error", FinishReason: provider.FinishReasonStop},
		},
	}
	executor := newLoopTestExecutor(errTool)
	loop := newTestLoop(p, executor, LoopConfig{MaxIterations: 5})

	resp, err := loop.Run(context.Background(), Request{
		Messages: []provider.LLMMessage{userMsg("do something")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StopReason != StopReasonComplete {
		t.Errorf("expected StopReasonComplete, got %s", resp.StopReason)
	}
	if len(resp.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if !resp.ToolCalls[0].Output.IsError {
		t.Error("expected tool call output to be an error")
	}
}

// TestRun_ParallelToolErrorIsolation: one tool errors in parallel, others succeed.
func TestRun_ParallelToolErrorIsolation(t *testing.T) {
	t.Parallel()

	goodTool := &mockTool{name: "good", output: tool.Output{Content: "ok"}}
	badTool := &mockTool{name: "bad", panicMsg: "boom"}

	p := &mockProvider{
		responses: []provider.CompletionResponse{
			{
				ToolCalls: []provider.ToolCall{
					{ID: "1", Name: "good", Arguments: json.RawMessage(`{}`)},
					{ID: "2", Name: "bad", Arguments: json.RawMessage(`{}`)},
				},
				FinishReason: provider.FinishReasonToolUse,
			},
			{Content: "handled", FinishReason: provider.FinishReasonStop},
		},
	}
	executor := newLoopTestExecutor(goodTool, badTool)
	loop := newTestLoop(p, executor, LoopConfig{MaxIterations: 5})

	resp, err := loop.Run(context.Background(), Request{
		Messages: []provider.LLMMessage{userMsg("parallel")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StopReason != StopReasonComplete {
		t.Errorf("expected StopReasonComplete, got %s", resp.StopReason)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Output.IsError {
		t.Error("expected first tool call to succeed")
	}
	if !resp.ToolCalls[1].Panicked || !resp.ToolCalls[1].Output.IsError {
		t.Error("expected second tool call to be a panicked error")
	}
}

// TestRun_MaxIterations: MaxIterations=2, provider always returns tool calls → StopReasonMaxIterations.
func TestRun_MaxIterations(t *testing.T) {
	t.Parallel()

	loopTool := &mockTool{name: "loop_tool", output: tool.Output{Content: "ok"}}
	// Provide more responses than MaxIterations to ensure the loop hits the limit.
	p := &mockProvider{
		responses: []provider.CompletionResponse{
			{ToolCalls: []provider.ToolCall{{ID: "1", Name: "loop_tool", Arguments: json.RawMessage(`{"i":1}`)}}, FinishReason: provider.FinishReasonToolUse},
			{ToolCalls: []provider.ToolCall{{ID: "2", Name: "loop_tool", Arguments: json.RawMessage(`{"i":2}`)}}, FinishReason: provider.FinishReasonToolUse},
			{ToolCalls: []provider.ToolCall{{ID: "3", Name: "loop_tool", Arguments: json.RawMessage(`{"i":3}`)}}, FinishReason: provider.FinishReasonToolUse},
		},
	}
	executor := newLoopTestExecutor(loopTool)
	loop := newTestLoop(p, executor, LoopConfig{MaxIterations: 2, LoopThreshold: 10})

	resp, err := loop.Run(context.Background(), Request{
		Messages: []provider.LLMMessage{userMsg("loop")},
	})

	if !errors.Is(err, ErrMaxIterationsReached) {
		t.Fatalf("expected ErrMaxIterationsReached, got %v", err)
	}
	if resp.StopReason != StopReasonMaxIterations {
		t.Errorf("expected StopReasonMaxIterations, got %s", resp.StopReason)
	}
	if resp.Iterations != 2 {
		t.Errorf("expected 2 iterations, got %d", resp.Iterations)
	}
}

// TestRun_LoopDetection: same tool+args repeated LoopThreshold times → StopReasonLoopDetected.
func TestRun_LoopDetection(t *testing.T) {
	t.Parallel()

	loopTool := &mockTool{name: "repeat_tool", output: tool.Output{Content: "same"}}
	sameArgs := json.RawMessage(`{"key":"value"}`)
	p := &mockProvider{
		responses: []provider.CompletionResponse{
			{ToolCalls: []provider.ToolCall{{ID: "1", Name: "repeat_tool", Arguments: sameArgs}}, FinishReason: provider.FinishReasonToolUse},
			{ToolCalls: []provider.ToolCall{{ID: "2", Name: "repeat_tool", Arguments: sameArgs}}, FinishReason: provider.FinishReasonToolUse},
			{ToolCalls: []provider.ToolCall{{ID: "3", Name: "repeat_tool", Arguments: sameArgs}}, FinishReason: provider.FinishReasonToolUse},
			{ToolCalls: []provider.ToolCall{{ID: "4", Name: "repeat_tool", Arguments: sameArgs}}, FinishReason: provider.FinishReasonToolUse},
		},
	}
	executor := newLoopTestExecutor(loopTool)
	loop := newTestLoop(p, executor, LoopConfig{MaxIterations: 10, LoopThreshold: 3})

	resp, err := loop.Run(context.Background(), Request{
		Messages: []provider.LLMMessage{userMsg("repeat")},
	})

	if !errors.Is(err, ErrLoopDetected) {
		t.Fatalf("expected ErrLoopDetected, got %v", err)
	}
	if resp.StopReason != StopReasonLoopDetected {
		t.Errorf("expected StopReasonLoopDetected, got %s", resp.StopReason)
	}
}

// TestRun_TokenBudget: TokenBudget=100, provider returns Usage{TotalTokens:150} → StopReasonTokenBudget on next iteration.
func TestRun_TokenBudget(t *testing.T) {
	t.Parallel()

	readTool := &mockTool{name: "read", output: tool.Output{Content: "data"}}
	p := &mockProvider{
		responses: []provider.CompletionResponse{
			{
				ToolCalls:    []provider.ToolCall{{ID: "1", Name: "read", Arguments: json.RawMessage(`{}`)}},
				FinishReason: provider.FinishReasonToolUse,
				Usage:        provider.TokenUsage{PromptTokens: 50, CompletionTokens: 100, TotalTokens: 150},
			},
			// This second response should never be reached because budget exceeded.
			{Content: "should not reach", FinishReason: provider.FinishReasonStop},
		},
	}
	executor := newLoopTestExecutor(readTool)
	loop := newTestLoop(p, executor, LoopConfig{MaxIterations: 10, TokenBudget: 100})

	resp, err := loop.Run(context.Background(), Request{
		Messages: []provider.LLMMessage{userMsg("use tokens")},
	})

	if !errors.Is(err, ErrTokenBudgetExceeded) {
		t.Fatalf("expected ErrTokenBudgetExceeded, got %v", err)
	}
	if resp.StopReason != StopReasonTokenBudget {
		t.Errorf("expected StopReasonTokenBudget, got %s", resp.StopReason)
	}
}

// TestRun_Timeout: use a context that's already cancelled → StopReasonTimeout.
func TestRun_Timeout(t *testing.T) {
	t.Parallel()

	p := &mockProvider{
		responses: []provider.CompletionResponse{
			{Content: "should not reach", FinishReason: provider.FinishReasonStop},
		},
	}
	executor := newLoopTestExecutor()
	loop := newTestLoop(p, executor, LoopConfig{MaxIterations: 5})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	resp, err := loop.Run(ctx, Request{
		Messages: []provider.LLMMessage{userMsg("hello")},
	})

	if err == nil {
		t.Fatal("expected an error for cancelled context")
	}
	if resp.StopReason != StopReasonTimeout {
		t.Errorf("expected StopReasonTimeout, got %s", resp.StopReason)
	}
}

// TestRun_PanicRecovery: tool panics → error result, loop continues.
func TestRun_PanicRecovery(t *testing.T) {
	t.Parallel()

	panicTool := &mockTool{name: "panic_tool", panicMsg: "unexpected panic"}
	p := &mockProvider{
		responses: []provider.CompletionResponse{
			{
				ToolCalls:    []provider.ToolCall{{ID: "1", Name: "panic_tool", Arguments: json.RawMessage(`{}`)}},
				FinishReason: provider.FinishReasonToolUse,
			},
			{Content: "recovered", FinishReason: provider.FinishReasonStop},
		},
	}
	executor := newLoopTestExecutor(panicTool)
	loop := newTestLoop(p, executor, LoopConfig{MaxIterations: 5})

	resp, err := loop.Run(context.Background(), Request{
		Messages: []provider.LLMMessage{userMsg("panic please")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StopReason != StopReasonComplete {
		t.Errorf("expected StopReasonComplete, got %s", resp.StopReason)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if !resp.ToolCalls[0].Panicked {
		t.Error("expected tool call to be marked as panicked")
	}
	if !resp.ToolCalls[0].Output.IsError {
		t.Error("expected panicked tool output to be an error")
	}
}

// --- Streaming tests ---

// TestRunStream_TextChunks: stream returns text chunks → StreamEventText events + StreamEventDone.
func TestRunStream_TextChunks(t *testing.T) {
	t.Parallel()

	p := &mockProvider{
		streams: [][]provider.StreamChunk{
			{
				{Content: "hello "},
				{Content: "world"},
				{FinishReason: provider.FinishReasonStop},
			},
		},
	}
	executor := newLoopTestExecutor()
	loop := newTestLoop(p, executor, LoopConfig{MaxIterations: 5})

	ch, err := loop.RunStream(context.Background(), Request{
		Messages: []provider.LLMMessage{userMsg("stream text")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var textContent string
	var gotDone bool
	for e := range ch {
		if e.Type == StreamEventText {
			textContent += e.Content
		}
		if e.Type == StreamEventDone {
			gotDone = true
		}
		if e.Type == StreamEventError {
			t.Fatalf("unexpected error event: %v", e.Err)
		}
	}

	if textContent != "hello world" {
		t.Errorf("expected text 'hello world', got %q", textContent)
	}
	if !gotDone {
		t.Error("expected StreamEventDone")
	}
}

// TestRunStream_ToolExecution: stream with tool calls → tool start/end events → final done.
func TestRunStream_ToolExecution(t *testing.T) {
	t.Parallel()

	readTool := &mockTool{name: "read", output: tool.Output{Content: "file data"}}
	p := &mockProvider{
		streams: [][]provider.StreamChunk{
			{
				{ToolCalls: []provider.ToolCall{{ID: "1", Name: "read", Arguments: json.RawMessage(`{}`)}}},
				{FinishReason: provider.FinishReasonToolUse},
			},
			{
				{Content: "got it"},
				{FinishReason: provider.FinishReasonStop},
			},
		},
	}
	executor := newLoopTestExecutor(readTool)
	loop := newTestLoop(p, executor, LoopConfig{MaxIterations: 5})

	ch, err := loop.RunStream(context.Background(), Request{
		Messages: []provider.LLMMessage{userMsg("read file")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var toolStarts, toolEnds int
	var gotDone bool
	for e := range ch {
		switch e.Type {
		case StreamEventToolStart:
			toolStarts++
		case StreamEventToolEnd:
			toolEnds++
		case StreamEventDone:
			gotDone = true
		case StreamEventError:
			t.Fatalf("unexpected error event: %v", e.Err)
		}
	}

	if toolStarts != 1 {
		t.Errorf("expected 1 tool start, got %d", toolStarts)
	}
	if toolEnds != 1 {
		t.Errorf("expected 1 tool end, got %d", toolEnds)
	}
	if !gotDone {
		t.Error("expected StreamEventDone")
	}
}

// TestRunStream_Done: simple stream completion with no tools.
func TestRunStream_Done(t *testing.T) {
	t.Parallel()

	p := &mockProvider{
		streams: [][]provider.StreamChunk{
			{
				{Content: "simple response"},
				{FinishReason: provider.FinishReasonStop},
			},
		},
	}
	executor := newLoopTestExecutor()
	loop := newTestLoop(p, executor, LoopConfig{MaxIterations: 5, Timeout: 10 * time.Second})

	ch, err := loop.RunStream(context.Background(), Request{
		Messages: []provider.LLMMessage{userMsg("simple")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var gotDone bool
	for e := range ch {
		if e.Type == StreamEventDone {
			gotDone = true
		}
		if e.Type == StreamEventError {
			t.Fatalf("unexpected error event: %v", e.Err)
		}
	}

	if !gotDone {
		t.Error("expected StreamEventDone")
	}
}

// TestRunStream_MaxIterations: streaming with max iterations guard.
func TestRunStream_MaxIterations(t *testing.T) {
	t.Parallel()

	readTool := &mockTool{name: "read", output: tool.Output{Content: "data"}}
	p := &mockProvider{
		streams: [][]provider.StreamChunk{
			{
				{ToolCalls: []provider.ToolCall{{ID: "1", Name: "read", Arguments: json.RawMessage(`{"i":1}`)}}},
				{FinishReason: provider.FinishReasonToolUse},
			},
			{
				{ToolCalls: []provider.ToolCall{{ID: "2", Name: "read", Arguments: json.RawMessage(`{"i":2}`)}}},
				{FinishReason: provider.FinishReasonToolUse},
			},
		},
	}
	executor := newLoopTestExecutor(readTool)
	loop := newTestLoop(p, executor, LoopConfig{MaxIterations: 1, LoopThreshold: 10})

	ch, err := loop.RunStream(context.Background(), Request{
		Messages: []provider.LLMMessage{userMsg("stream loop")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var gotMaxIter bool
	for e := range ch {
		if e.Type == StreamEventError && errors.Is(e.Err, ErrMaxIterationsReached) {
			gotMaxIter = true
		}
	}
	if !gotMaxIter {
		t.Error("expected StreamEventError with ErrMaxIterationsReached")
	}
}

// TestRunStream_TokenBudget: streaming with token budget guard.
func TestRunStream_TokenBudget(t *testing.T) {
	t.Parallel()

	readTool := &mockTool{name: "read", output: tool.Output{Content: "data"}}
	p := &mockProvider{
		streams: [][]provider.StreamChunk{
			{
				{ToolCalls: []provider.ToolCall{{ID: "1", Name: "read", Arguments: json.RawMessage(`{}`)}}},
				{Usage: &provider.TokenUsage{PromptTokens: 50, CompletionTokens: 100, TotalTokens: 150}},
				{FinishReason: provider.FinishReasonToolUse},
			},
			// Should not be reached.
			{
				{Content: "unreachable"},
				{FinishReason: provider.FinishReasonStop},
			},
		},
	}
	executor := newLoopTestExecutor(readTool)
	loop := newTestLoop(p, executor, LoopConfig{MaxIterations: 10, TokenBudget: 100})

	ch, err := loop.RunStream(context.Background(), Request{
		Messages: []provider.LLMMessage{userMsg("use tokens")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var gotBudget bool
	for e := range ch {
		if e.Type == StreamEventError && errors.Is(e.Err, ErrTokenBudgetExceeded) {
			gotBudget = true
		}
	}
	if !gotBudget {
		t.Error("expected StreamEventError with ErrTokenBudgetExceeded")
	}
}

// TestRunStream_LoopDetection: streaming with loop detection guard.
func TestRunStream_LoopDetection(t *testing.T) {
	t.Parallel()

	readTool := &mockTool{name: "repeat", output: tool.Output{Content: "same"}}
	sameArgs := json.RawMessage(`{"key":"value"}`)
	p := &mockProvider{
		streams: [][]provider.StreamChunk{
			{
				{ToolCalls: []provider.ToolCall{{ID: "1", Name: "repeat", Arguments: sameArgs}}},
				{FinishReason: provider.FinishReasonToolUse},
			},
			{
				{ToolCalls: []provider.ToolCall{{ID: "2", Name: "repeat", Arguments: sameArgs}}},
				{FinishReason: provider.FinishReasonToolUse},
			},
			{
				{ToolCalls: []provider.ToolCall{{ID: "3", Name: "repeat", Arguments: sameArgs}}},
				{FinishReason: provider.FinishReasonToolUse},
			},
		},
	}
	executor := newLoopTestExecutor(readTool)
	loop := newTestLoop(p, executor, LoopConfig{MaxIterations: 10, LoopThreshold: 3})

	ch, err := loop.RunStream(context.Background(), Request{
		Messages: []provider.LLMMessage{userMsg("repeat")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var gotLoop bool
	for e := range ch {
		if e.Type == StreamEventError && errors.Is(e.Err, ErrLoopDetected) {
			gotLoop = true
		}
	}
	if !gotLoop {
		t.Error("expected StreamEventError with ErrLoopDetected")
	}
}

// TestRunStream_Timeout: streaming with cancelled context.
func TestRunStream_Timeout(t *testing.T) {
	t.Parallel()

	p := &mockProvider{
		streams: [][]provider.StreamChunk{
			{
				{Content: "should not reach"},
				{FinishReason: provider.FinishReasonStop},
			},
		},
	}
	executor := newLoopTestExecutor()
	loop := newTestLoop(p, executor, LoopConfig{MaxIterations: 5})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	ch, err := loop.RunStream(ctx, Request{
		Messages: []provider.LLMMessage{userMsg("timeout")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var gotTimeout bool
	for e := range ch {
		if e.Type == StreamEventError {
			gotTimeout = true
		}
	}
	if !gotTimeout {
		t.Error("expected StreamEventError for cancelled context")
	}
}
