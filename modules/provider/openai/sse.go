package openai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/flemzord/sclaw/internal/provider"
)

// maxToolCallArgs is the maximum accumulated size in bytes for a single
// tool call's arguments during streaming. Protects against OOM from a
// malicious or broken upstream sending unbounded argument fragments.
const maxToolCallArgs = 1 * 1024 * 1024 // 1 MB

// scannerBufferSize is the max token size for the SSE line scanner.
// OpenAI SSE data lines can be large (tool call arguments, long content).
// Default bufio.Scanner limit is ~64 KiB which is too small.
const scannerBufferSize = 1 * 1024 * 1024 // 1 MB

// toolCallDelta accumulates streaming tool call fragments.
type toolCallDelta struct {
	id   string
	name string
	args strings.Builder
}

// sendChunk sends a StreamChunk on ch, respecting context cancellation.
// Returns false if the context was cancelled (caller should return).
func sendChunk(ctx context.Context, ch chan<- provider.StreamChunk, chunk provider.StreamChunk) bool {
	select {
	case ch <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}

// readStream reads an SSE stream from body and sends parsed chunks on ch.
// The channel is closed when the stream ends, either normally ([DONE]),
// on error, or when ctx is cancelled. body is always closed.
func readStream(ctx context.Context, body io.ReadCloser, ch chan<- provider.StreamChunk) {
	defer close(ch)
	defer func() { _ = body.Close() }()

	// Close body on context cancellation to unblock the scanner.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = body.Close()
		case <-done:
		}
	}()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, scannerBufferSize), scannerBufferSize)
	pending := make(map[int]*toolCallDelta)

	for scanner.Scan() {
		// Check context cancellation after unblocking.
		if ctx.Err() != nil {
			sendChunk(ctx, ch, provider.StreamChunk{Err: ctx.Err()})
			return
		}

		line := scanner.Text()

		// SSE spec: lines starting with ":" are comments.
		if strings.HasPrefix(line, ":") {
			continue
		}

		// Only process "data:" lines.
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}

		// Terminal marker.
		if data == "[DONE]" {
			// Flush any accumulated tool calls.
			if len(pending) > 0 {
				sendChunk(ctx, ch, provider.StreamChunk{
					ToolCalls: assembleToolCalls(pending),
				})
			}
			return
		}

		var chunk chatStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			sendChunk(ctx, ch, provider.StreamChunk{Err: err})
			return
		}

		// Process usage if present (sent with stream_options.include_usage).
		var usage *provider.TokenUsage
		if chunk.Usage != nil {
			usage = &provider.TokenUsage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
		}

		if len(chunk.Choices) == 0 {
			// Usage-only chunk (final chunk with include_usage).
			if usage != nil {
				sendChunk(ctx, ch, provider.StreamChunk{Usage: usage})
			}
			continue
		}

		choice := chunk.Choices[0]

		// Accumulate tool call deltas with a per-call size cap.
		for _, tc := range choice.Delta.ToolCalls {
			delta, ok := pending[tc.Index]
			if !ok {
				delta = &toolCallDelta{}
				pending[tc.Index] = delta
			}
			if tc.ID != "" {
				delta.id = tc.ID
			}
			if tc.Function.Name != "" {
				delta.name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				if delta.args.Len()+len(tc.Function.Arguments) > maxToolCallArgs {
					sendChunk(ctx, ch, provider.StreamChunk{
						Err: fmt.Errorf("openai: tool call arguments exceeded %d bytes", maxToolCallArgs),
					})
					return
				}
				delta.args.WriteString(tc.Function.Arguments)
			}
		}

		// Emit content chunks.
		if choice.Delta.Content != "" {
			if !sendChunk(ctx, ch, provider.StreamChunk{
				Content: choice.Delta.Content,
				Usage:   usage,
			}) {
				return
			}
			continue
		}

		// On finish_reason, flush tool calls.
		if choice.FinishReason != nil {
			sc := provider.StreamChunk{
				FinishReason: mapFinishReason(choice.FinishReason),
				Usage:        usage,
			}
			if len(pending) > 0 {
				sc.ToolCalls = assembleToolCalls(pending)
				// Reset pending after flushing.
				pending = make(map[int]*toolCallDelta)
			}
			if !sendChunk(ctx, ch, sc) {
				return
			}
			continue
		}

		// Usage-only chunk attached to a choice with no content/finish.
		if usage != nil {
			sendChunk(ctx, ch, provider.StreamChunk{Usage: usage})
		}
	}

	// If scanner stopped due to context cancellation (body closed), report context error.
	if ctx.Err() != nil {
		sendChunk(ctx, ch, provider.StreamChunk{Err: ctx.Err()})
		return
	}

	// Map scanner errors through mapConnectionError so that network failures
	// are reported as ErrProviderDown and can trigger health degradation.
	if err := scanner.Err(); err != nil {
		sendChunk(ctx, ch, provider.StreamChunk{Err: mapConnectionError(err)})
	}
}

// assembleToolCalls converts accumulated tool call deltas into provider
// ToolCalls, sorted by their stream index.
func assembleToolCalls(pending map[int]*toolCallDelta) []provider.ToolCall {
	type indexed struct {
		idx  int
		call provider.ToolCall
	}
	items := make([]indexed, 0, len(pending))
	for idx, delta := range pending {
		items = append(items, indexed{
			idx: idx,
			call: provider.ToolCall{
				ID:        delta.id,
				Name:      delta.name,
				Arguments: json.RawMessage(delta.args.String()),
			},
		})
	}
	slices.SortFunc(items, func(a, b indexed) int {
		return a.idx - b.idx
	})
	out := make([]provider.ToolCall, len(items))
	for i, item := range items {
		out[i] = item.call
	}
	return out
}
