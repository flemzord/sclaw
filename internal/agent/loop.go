package agent

import (
	"context"
	"errors"
	"strings"

	"github.com/flemzord/sclaw/internal/provider"
)

// Sentinel errors for agent loop termination.
var (
	ErrTokenBudgetExceeded  = errors.New("agent: token budget exceeded")
	ErrMaxIterationsReached = errors.New("agent: max iterations reached")
	ErrLoopDetected         = errors.New("agent: loop detected")
)

// Loop implements the ReAct (Reason + Act) reasoning loop.
type Loop struct {
	provider provider.Provider
	executor *ToolExecutor
	config   LoopConfig
}

// NewLoop creates a Loop with the given provider, executor, and config.
func NewLoop(p provider.Provider, executor *ToolExecutor, cfg LoopConfig) *Loop {
	return &Loop{
		provider: p,
		executor: executor,
		config:   cfg.withDefaults(),
	}
}

// buildInitialMessages assembles the initial message history from the request.
func buildInitialMessages(req Request) []provider.LLMMessage {
	var messages []provider.LLMMessage
	if req.SystemPrompt != "" {
		messages = append(messages, provider.LLMMessage{
			Role:    provider.MessageRoleSystem,
			Content: req.SystemPrompt,
		})
	}
	return append(messages, req.Messages...)
}

// appendToolResults adds tool execution results to the conversation history.
func appendToolResults(messages []provider.LLMMessage, records []ToolCallRecord) []provider.LLMMessage {
	for _, rec := range records {
		messages = append(messages, provider.LLMMessage{
			Role:    provider.MessageRoleTool,
			Content: rec.Output.Content,
			ToolID:  rec.ID,
			IsError: rec.Output.IsError,
		})
	}
	return messages
}

func appendAssistantMessage(
	messages []provider.LLMMessage,
	content string,
	toolCalls []provider.ToolCall,
) []provider.LLMMessage {
	return append(messages, provider.LLMMessage{
		Role:      provider.MessageRoleAssistant,
		Content:   content,
		ToolCalls: toolCalls,
	})
}

func emitStreamEvent(ctx context.Context, ch chan<- StreamEvent, event StreamEvent) bool {
	// Fast path: emit immediately if the buffer/receiver is available.
	select {
	case ch <- event:
		return true
	default:
	}

	// Slow path: wait for either a receiver or context cancellation.
	select {
	case ch <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

// Run executes the ReAct loop synchronously and returns the final response.
//
// A context.WithTimeout is applied using l.config.Timeout. If the caller's
// context already carries a shorter deadline, the shorter one takes effect.
func (l *Loop) Run(ctx context.Context, req Request) (Response, error) {
	ctx, cancel := context.WithTimeout(ctx, l.config.Timeout)
	defer cancel()

	detector := newLoopDetector(l.config.LoopThreshold)
	tracker := newTokenTracker(l.config.TokenBudget)
	messages := buildInitialMessages(req)

	var allToolCalls []ToolCallRecord

	for i := 0; i < l.config.MaxIterations; i++ {
		// Check context cancellation (timeout or external cancel).
		if err := ctx.Err(); err != nil {
			stopReason := StopReasonError
			if errors.Is(err, context.DeadlineExceeded) {
				stopReason = StopReasonTimeout
			}
			return Response{
				ToolCalls:  allToolCalls,
				TotalUsage: tracker.total(),
				Iterations: i,
				StopReason: stopReason,
			}, err
		}

		// Call provider.
		resp, err := l.provider.Complete(ctx, provider.CompletionRequest{
			Messages: messages,
			Tools:    req.Tools,
		})
		if err != nil {
			return Response{
				ToolCalls:  allToolCalls,
				TotalUsage: tracker.total(),
				Iterations: i,
				StopReason: StopReasonError,
			}, err
		}

		tracker.add(resp.Usage)
		if tracker.exceeded() {
			return Response{
				ToolCalls:  allToolCalls,
				TotalUsage: tracker.total(),
				Iterations: i + 1,
				StopReason: StopReasonTokenBudget,
			}, ErrTokenBudgetExceeded
		}

		// No tool calls → the model is done reasoning.
		if len(resp.ToolCalls) == 0 {
			return Response{
				Content:    resp.Content,
				ToolCalls:  allToolCalls,
				TotalUsage: tracker.total(),
				Iterations: i + 1,
				StopReason: StopReasonComplete,
			}, nil
		}

		// Check for loops before appending assistant message to avoid
		// leaving an orphan assistant message without tool results.
		for _, tc := range resp.ToolCalls {
			if detector.record(tc.Name, tc.Arguments) {
				return Response{
					ToolCalls:  allToolCalls,
					TotalUsage: tracker.total(),
					Iterations: i + 1,
					StopReason: StopReasonLoopDetected,
				}, ErrLoopDetected
			}
		}

		// Append assistant message with content and tool calls.
		messages = appendAssistantMessage(messages, resp.Content, resp.ToolCalls)

		// Execute tools in parallel.
		records := l.executor.Execute(ctx, resp.ToolCalls)
		allToolCalls = append(allToolCalls, records...)

		// Re-inject tool results into conversation.
		messages = appendToolResults(messages, records)
	}

	// Max iterations reached.
	return Response{
		ToolCalls:  allToolCalls,
		TotalUsage: tracker.total(),
		Iterations: l.config.MaxIterations,
		StopReason: StopReasonMaxIterations,
	}, ErrMaxIterationsReached
}

// RunStream executes the ReAct loop and streams events over a channel.
//
// A context.WithTimeout is applied using l.config.Timeout. If the caller's
// context already carries a shorter deadline, the shorter one takes effect.
//
// The caller should either drain the returned channel until close or cancel
// the context; otherwise the producer goroutine may block on sends.
func (l *Loop) RunStream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, 16)

	go func() {
		defer close(ch)

		ctx, cancel := context.WithTimeout(ctx, l.config.Timeout)
		defer cancel()

		detector := newLoopDetector(l.config.LoopThreshold)
		tracker := newTokenTracker(l.config.TokenBudget)
		messages := buildInitialMessages(req)
		var allToolCalls []ToolCallRecord

		for i := 0; i < l.config.MaxIterations; i++ {
			if err := ctx.Err(); err != nil {
				// Best-effort emit: context is already cancelled so the
				// select on ctx.Done inside emitStreamEvent would fail if
				// the buffer is full. Use a non-blocking send instead.
				select {
				case ch <- StreamEvent{Type: StreamEventError, Err: err}:
				default:
				}
				return
			}

			streamCh, err := l.provider.Stream(ctx, provider.CompletionRequest{
				Messages: messages,
				Tools:    req.Tools,
			})
			if err != nil {
				emitStreamEvent(ctx, ch, StreamEvent{Type: StreamEventError, Err: err})
				return
			}

			// Consume stream, forwarding text chunks and accumulating tool calls.
			var content strings.Builder
			var toolCalls []provider.ToolCall
			var usage *provider.TokenUsage

			var streamErr error
			for chunk := range streamCh {
				if chunk.Err != nil {
					streamErr = chunk.Err
					break
				}
				if chunk.Content != "" {
					content.WriteString(chunk.Content)
					if !emitStreamEvent(ctx, ch, StreamEvent{Type: StreamEventText, Content: chunk.Content}) {
						return
					}
				}
				if len(chunk.ToolCalls) > 0 {
					toolCalls = append(toolCalls, chunk.ToolCalls...)
				}
				if chunk.Usage != nil {
					usage = chunk.Usage
				}
			}

			// Drain remaining chunks to prevent provider goroutine leak.
			if streamErr != nil {
				//nolint:revive // intentional empty drain loop
				for range streamCh { //nolint:revive
				}
				emitStreamEvent(ctx, ch, StreamEvent{Type: StreamEventError, Err: streamErr})
				return
			}

			if usage != nil {
				tracker.add(*usage)
				if !emitStreamEvent(ctx, ch, StreamEvent{Type: StreamEventUsage, Usage: usage}) {
					return
				}
			}

			if tracker.exceeded() {
				emitStreamEvent(ctx, ch, StreamEvent{Type: StreamEventError, Err: ErrTokenBudgetExceeded})
				return
			}

			// No tool calls → done.
			if len(toolCalls) == 0 {
				final := Response{
					Content:    content.String(),
					ToolCalls:  allToolCalls,
					TotalUsage: tracker.total(),
					Iterations: i + 1,
					StopReason: StopReasonComplete,
				}
				emitStreamEvent(ctx, ch, StreamEvent{Type: StreamEventDone, Final: &final})
				return
			}

			// Check loops before appending assistant message to avoid
			// leaving an orphan assistant message without tool results.
			for _, tc := range toolCalls {
				if detector.record(tc.Name, tc.Arguments) {
					emitStreamEvent(ctx, ch, StreamEvent{Type: StreamEventError, Err: ErrLoopDetected})
					return
				}
			}

			messages = appendAssistantMessage(messages, content.String(), toolCalls)

			// Signal tool starts.
			for _, tc := range toolCalls {
				if !emitStreamEvent(ctx, ch, StreamEvent{
					Type:     StreamEventToolStart,
					ToolCall: &ToolCallRecord{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments},
				}) {
					return
				}
			}

			records := l.executor.Execute(ctx, toolCalls)
			allToolCalls = append(allToolCalls, records...)

			for idx := range records {
				if !emitStreamEvent(ctx, ch, StreamEvent{
					Type:     StreamEventToolEnd,
					ToolCall: &records[idx],
				}) {
					return
				}
			}

			// Re-inject tool results.
			messages = appendToolResults(messages, records)
		}

		emitStreamEvent(ctx, ch, StreamEvent{Type: StreamEventError, Err: ErrMaxIterationsReached})
	}()

	return ch, nil
}
