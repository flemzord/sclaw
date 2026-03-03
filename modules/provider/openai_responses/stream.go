package openairesponses

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/coder/websocket"
	"github.com/flemzord/sclaw/internal/provider"
)

// toolAccumulator collects streaming tool call deltas by output index.
type toolAccumulator struct {
	calls map[int]*provider.ToolCall
}

func newToolAccumulator() *toolAccumulator {
	return &toolAccumulator{calls: make(map[int]*provider.ToolCall)}
}

// addArgDelta appends argument text to the tool call at the given index.
func (ta *toolAccumulator) addArgDelta(index int, delta string) {
	tc, ok := ta.calls[index]
	if !ok {
		tc = &provider.ToolCall{}
		ta.calls[index] = tc
	}
	tc.Arguments = append(tc.Arguments, delta...)
}

// setItem sets the full tool call metadata from a completed output item.
func (ta *toolAccumulator) setItem(index int, item outputItem) {
	tc, ok := ta.calls[index]
	if !ok {
		tc = &provider.ToolCall{}
		ta.calls[index] = tc
	}
	tc.ID = item.CallID
	tc.Name = item.Name
	if tc.Arguments == nil {
		tc.Arguments = json.RawMessage(item.Arguments)
	}
}

// result returns the accumulated tool calls in index order.
func (ta *toolAccumulator) result() []provider.ToolCall {
	if len(ta.calls) == 0 {
		return nil
	}
	maxIdx := 0
	for idx := range ta.calls {
		if idx > maxIdx {
			maxIdx = idx
		}
	}
	result := make([]provider.ToolCall, 0, len(ta.calls))
	for i := 0; i <= maxIdx; i++ {
		if tc, ok := ta.calls[i]; ok {
			result = append(result, *tc)
		}
	}
	return result
}

// readLoop reads WebSocket messages and emits StreamChunks on the returned channel.
// The channel is closed when the response completes or an error occurs.
func readLoop(ctx context.Context, conn *websocket.Conn) <-chan provider.StreamChunk {
	ch := make(chan provider.StreamChunk, 16)

	go func() {
		defer close(ch)

		tools := newToolAccumulator()

		for {
			if err := ctx.Err(); err != nil {
				emit(ctx, ch, provider.StreamChunk{Err: err})
				return
			}

			_, data, err := conn.Read(ctx)
			if err != nil {
				// Do not classify caller cancellation as provider failure.
				if ctx.Err() != nil {
					emit(ctx, ch, provider.StreamChunk{Err: ctx.Err()})
				} else {
					emit(ctx, ch, provider.StreamChunk{
						Err: fmt.Errorf("%w: WebSocket read: %w", provider.ErrProviderDown, err),
					})
				}
				return
			}

			var event serverEvent
			if err := json.Unmarshal(data, &event); err != nil {
				emit(ctx, ch, provider.StreamChunk{
					Err: fmt.Errorf("parse server event: %w", err),
				})
				return
			}

			switch event.Type {
			case "response.output_text.delta":
				if event.Delta != "" {
					emit(ctx, ch, provider.StreamChunk{Content: event.Delta})
				}

			case "response.function_call_arguments.delta":
				tools.addArgDelta(event.OutputIndex, event.Delta)

			case "response.output_item.done":
				if event.Item != nil && event.Item.Type == "function_call" {
					tools.setItem(event.OutputIndex, *event.Item)
				}

			case "response.completed":
				if event.Response == nil {
					emit(ctx, ch, provider.StreamChunk{
						Err: fmt.Errorf("response.completed with nil response payload"),
					})
					return
				}

				// Emit accumulated tool calls.
				if tcs := tools.result(); len(tcs) > 0 {
					emit(ctx, ch, provider.StreamChunk{ToolCalls: tcs})
				}

				// Emit final chunk with finish reason and usage.
				final := provider.StreamChunk{}
				stopReason := event.Response.StopReason
				if stopReason == "" {
					stopReason = event.Response.FinishReason
				}
				if stopReason == "" {
					stopReason = event.Response.Status
				}
				final.FinishReason = mapStopReason(stopReason, len(tools.calls) > 0)

				if event.Response.Usage != nil {
					final.Usage = &provider.TokenUsage{
						PromptTokens:     event.Response.Usage.InputTokens,
						CompletionTokens: event.Response.Usage.OutputTokens,
						TotalTokens:      event.Response.Usage.TotalTokens,
					}
				}

				emit(ctx, ch, final)
				return

			case "error":
				if event.Error != nil {
					emit(ctx, ch, provider.StreamChunk{Err: classifyServerError(event.Error)})
				} else {
					emit(ctx, ch, provider.StreamChunk{
						Err: fmt.Errorf("%w: unknown server error", provider.ErrProviderDown),
					})
				}
				return

			// Ignored events: response.created, response.output_item.added,
			// response.content_part.added, response.content_part.done,
			// response.output_text.done, response.function_call_arguments.done,
			// rate_limits.updated, etc.
			default:
				continue
			}
		}
	}()

	return ch
}

// emit sends a chunk on ch. For error chunks when the context is already
// cancelled, it uses a non-blocking send to avoid losing the error
// (the buffered channel should have room).
func emit(ctx context.Context, ch chan<- provider.StreamChunk, chunk provider.StreamChunk) {
	if chunk.Err != nil && ctx.Err() != nil {
		// Context is already done; a blocking select would race between
		// ch<- and ctx.Done and might drop the error. Use non-blocking send.
		select {
		case ch <- chunk:
		default:
		}
		return
	}
	select {
	case ch <- chunk:
	case <-ctx.Done():
	}
}

// classifyServerError maps a Responses API error to a provider sentinel error.
func classifyServerError(se *serverError) error {
	switch se.Code {
	case "rate_limit_exceeded":
		return fmt.Errorf("%w: %s", provider.ErrRateLimit, se.Message)
	case "context_length_exceeded", "invalid_prompt":
		return fmt.Errorf("%w: %s", provider.ErrContextLength, se.Message)
	case "server_error", "overloaded":
		return fmt.Errorf("%w: %s", provider.ErrProviderDown, se.Message)
	case "invalid_api_key", "authentication_error":
		return fmt.Errorf("%w: %s", provider.ErrAuthentication, se.Message)
	default:
		return fmt.Errorf("%w: [%s] %s", provider.ErrProviderDown, se.Code, se.Message)
	}
}
