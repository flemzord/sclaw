package agent

import (
	"context"
	"errors"

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
		})
	}
	return messages
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

		// Check token budget.
		if tracker.exceeded() {
			return Response{
				ToolCalls:  allToolCalls,
				TotalUsage: tracker.total(),
				Iterations: i,
				StopReason: StopReasonTokenBudget,
			}, ErrTokenBudgetExceeded
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

		// Append assistant message with the content (may be empty).
		messages = append(messages, provider.LLMMessage{
			Role:    provider.MessageRoleAssistant,
			Content: resp.Content,
		})

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
func (l *Loop) RunStream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, 16)

	go func() {
		defer close(ch)

		ctx, cancel := context.WithTimeout(ctx, l.config.Timeout)
		defer cancel()

		detector := newLoopDetector(l.config.LoopThreshold)
		tracker := newTokenTracker(l.config.TokenBudget)
		messages := buildInitialMessages(req)

		for i := 0; i < l.config.MaxIterations; i++ {
			if err := ctx.Err(); err != nil {
				ch <- StreamEvent{Type: StreamEventError, Err: err}
				return
			}

			if tracker.exceeded() {
				ch <- StreamEvent{Type: StreamEventError, Err: ErrTokenBudgetExceeded}
				return
			}

			streamCh, err := l.provider.Stream(ctx, provider.CompletionRequest{
				Messages: messages,
				Tools:    req.Tools,
			})
			if err != nil {
				ch <- StreamEvent{Type: StreamEventError, Err: err}
				return
			}

			// Consume stream, forwarding text chunks and accumulating tool calls.
			var content string
			var toolCalls []provider.ToolCall
			var usage *provider.TokenUsage

			var streamErr error
			for chunk := range streamCh {
				if chunk.Err != nil {
					streamErr = chunk.Err
					break
				}
				if chunk.Content != "" {
					content += chunk.Content
					ch <- StreamEvent{Type: StreamEventText, Content: chunk.Content}
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
				ch <- StreamEvent{Type: StreamEventError, Err: streamErr}
				return
			}

			if usage != nil {
				tracker.add(*usage)
				ch <- StreamEvent{Type: StreamEventUsage, Usage: usage}
				if tracker.exceeded() {
					ch <- StreamEvent{Type: StreamEventError, Err: ErrTokenBudgetExceeded}
					return
				}
			}

			// No tool calls → done.
			if len(toolCalls) == 0 {
				ch <- StreamEvent{Type: StreamEventDone}
				return
			}

			// Check loops before appending assistant message to avoid
			// leaving an orphan assistant message without tool results.
			for _, tc := range toolCalls {
				if detector.record(tc.Name, tc.Arguments) {
					ch <- StreamEvent{Type: StreamEventError, Err: ErrLoopDetected}
					return
				}
			}

			messages = append(messages, provider.LLMMessage{
				Role:    provider.MessageRoleAssistant,
				Content: content,
			})

			// Signal tool starts.
			for _, tc := range toolCalls {
				ch <- StreamEvent{
					Type:     StreamEventToolStart,
					ToolCall: &ToolCallRecord{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments},
				}
			}

			records := l.executor.Execute(ctx, toolCalls)

			for idx := range records {
				ch <- StreamEvent{
					Type:     StreamEventToolEnd,
					ToolCall: &records[idx],
				}
			}

			// Re-inject tool results.
			messages = appendToolResults(messages, records)
		}

		ch <- StreamEvent{Type: StreamEventError, Err: ErrMaxIterationsReached}
	}()

	return ch, nil
}
