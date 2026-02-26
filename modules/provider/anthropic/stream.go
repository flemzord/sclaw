package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	sdkanthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
	"github.com/flemzord/sclaw/internal/provider"
)

// maxToolBuffers is the maximum number of concurrent tool_use content blocks
// tracked during a single stream. This bounds memory in case of a misbehaving
// server that sends unbounded ContentBlockStart events without matching Stop events.
const maxToolBuffers = 100

// streamBufferSize matches the agent loop's channel buffer (loop.go:193).
const streamBufferSize = 16

// Stream sends a streaming completion request and returns a channel of StreamChunks.
// The channel is closed when the stream ends or an error occurs.
// Initial connection errors are returned directly; mid-stream errors arrive via StreamChunk.Err.
func (a *Anthropic) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	params := convertRequest(req, &a.config, a.logger)

	stream := a.client.Messages.NewStreaming(ctx, params)

	// Consume the first event synchronously to surface initial connection
	// errors (auth, network, 4xx) directly to the caller, as required by
	// the Provider interface contract. This enables the chain to failover.
	if !stream.Next() {
		err := stream.Err()
		_ = stream.Close() //nolint:errcheck // best-effort close
		if err != nil {
			return nil, mapError(err)
		}
		// Stream ended without error or events — return an empty closed channel.
		ch := make(chan provider.StreamChunk)
		close(ch)
		return ch, nil
	}

	firstEvent := stream.Current()

	ch := make(chan provider.StreamChunk, streamBufferSize)

	go func() {
		defer close(ch)
		defer func() { _ = stream.Close() }() //nolint:errcheck // best-effort close

		a.consumeStreamWithFirst(ctx, stream, firstEvent, ch)
	}()

	return ch, nil
}

// streamState tracks accumulated state across SSE events for a single stream.
type streamState struct {
	// inputTokens captured from MessageStartEvent.
	inputTokens int64

	// toolBuffers accumulates JSON arguments per content block index.
	toolBuffers map[int64]*toolBuffer
}

// toolBuffer accumulates a tool_use content block's data across deltas.
type toolBuffer struct {
	id   string
	name string
	args strings.Builder
}

// consumeStreamWithFirst processes the already-consumed first event, then
// continues consuming the rest of the stream.
func (a *Anthropic) consumeStreamWithFirst(
	ctx context.Context,
	stream *ssestream.Stream[sdkanthropic.MessageStreamEventUnion],
	firstEvent sdkanthropic.MessageStreamEventUnion,
	ch chan<- provider.StreamChunk,
) {
	state := streamState{
		toolBuffers: make(map[int64]*toolBuffer),
	}

	// Process the first event that was consumed during Stream().
	a.processEvent(ctx, &state, firstEvent, ch)

	for stream.Next() {
		if ctx.Err() != nil {
			return
		}
		a.processEvent(ctx, &state, stream.Current(), ch)
	}

	if err := stream.Err(); err != nil {
		emit(ctx, ch, provider.StreamChunk{Err: mapError(err)})
	}
}

// processEvent dispatches a single SSE event to the appropriate handler.
func (a *Anthropic) processEvent(
	ctx context.Context,
	state *streamState,
	event sdkanthropic.MessageStreamEventUnion,
	ch chan<- provider.StreamChunk,
) {
	switch ev := event.AsAny().(type) {
	case sdkanthropic.MessageStartEvent:
		state.inputTokens = ev.Message.Usage.InputTokens

	case sdkanthropic.ContentBlockStartEvent:
		a.handleBlockStart(ctx, state, ev, ch)

	case sdkanthropic.ContentBlockDeltaEvent:
		a.handleBlockDelta(ctx, state, ev, ch)

	case sdkanthropic.ContentBlockStopEvent:
		a.handleBlockStop(ctx, state, ev, ch)

	case sdkanthropic.MessageDeltaEvent:
		a.handleMessageDelta(ctx, state, ev, ch)
	}
}

// handleBlockStart initializes tracking for a new content block.
func (a *Anthropic) handleBlockStart(ctx context.Context, state *streamState, ev sdkanthropic.ContentBlockStartEvent, ch chan<- provider.StreamChunk) {
	if ev.ContentBlock.Type != "tool_use" {
		return
	}
	if len(state.toolBuffers) >= maxToolBuffers {
		emit(ctx, ch, provider.StreamChunk{
			Err: fmt.Errorf("provider.anthropic: exceeded max tool buffers (%d)", maxToolBuffers),
		})
		return
	}
	state.toolBuffers[ev.Index] = &toolBuffer{
		id:   ev.ContentBlock.ID,
		name: ev.ContentBlock.Name,
	}
}

// handleBlockDelta processes incremental content: text deltas are emitted
// immediately, tool input JSON is accumulated in the buffer.
func (a *Anthropic) handleBlockDelta(
	ctx context.Context,
	state *streamState,
	ev sdkanthropic.ContentBlockDeltaEvent,
	ch chan<- provider.StreamChunk,
) {
	switch delta := ev.Delta.AsAny().(type) {
	case sdkanthropic.TextDelta:
		emit(ctx, ch, provider.StreamChunk{Content: delta.Text})

	case sdkanthropic.InputJSONDelta:
		if buf, ok := state.toolBuffers[ev.Index]; ok {
			buf.args.WriteString(delta.PartialJSON)
		}
	}
}

// handleBlockStop emits a complete tool call when a tool_use block ends.
func (a *Anthropic) handleBlockStop(
	ctx context.Context,
	state *streamState,
	ev sdkanthropic.ContentBlockStopEvent,
	ch chan<- provider.StreamChunk,
) {
	buf, ok := state.toolBuffers[ev.Index]
	if !ok {
		return
	}

	args := json.RawMessage(buf.args.String())
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}

	emit(ctx, ch, provider.StreamChunk{
		ToolCalls: []provider.ToolCall{{
			ID:        buf.id,
			Name:      buf.name,
			Arguments: args,
		}},
	})

	delete(state.toolBuffers, ev.Index)
}

// handleMessageDelta emits the final usage and finish reason.
func (a *Anthropic) handleMessageDelta(
	ctx context.Context,
	state *streamState,
	ev sdkanthropic.MessageDeltaEvent,
	ch chan<- provider.StreamChunk,
) {
	outputTokens := ev.Usage.OutputTokens
	inputTokens := state.inputTokens

	emit(ctx, ch, provider.StreamChunk{
		FinishReason: convertStopReason(ev.Delta.StopReason),
		Usage: &provider.TokenUsage{
			PromptTokens:     int(inputTokens),
			CompletionTokens: int(outputTokens),
			TotalTokens:      int(inputTokens + outputTokens),
		},
	})
}

// emit sends a StreamChunk to the channel, respecting context cancellation.
func emit(ctx context.Context, ch chan<- provider.StreamChunk, chunk provider.StreamChunk) {
	select {
	case ch <- chunk:
	case <-ctx.Done():
	}
}
