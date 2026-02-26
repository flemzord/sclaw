package openrouter

import (
	"bufio"
	"encoding/json"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/flemzord/sclaw/internal/provider"
)

// sseMaxLineSize is the maximum SSE line size (512 KiB). Large tool call
// arguments or code outputs can exceed the default 64 KiB bufio.Scanner limit.
const sseMaxLineSize = 512 * 1024

// parseSSE reads an SSE stream from r and sends decoded chunks to ch.
// It handles OpenRouter-specific keepalive comments, the [DONE] sentinel,
// and mid-stream error objects. The caller must close ch after parseSSE returns.
//
// Note: this parser assumes each data payload fits on a single "data:" line,
// which is the format used by all OpenAI-compatible APIs. Multi-line SSE data
// fields (per the SSE spec) are not supported.
func parseSSE(r io.Reader, ch chan<- provider.StreamChunk) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, sseMaxLineSize), sseMaxLineSize)

	// toolArgs accumulates streamed tool call arguments by index.
	var toolArgs map[int]*toolCallAccumulator

	for scanner.Scan() {
		line := scanner.Text()

		// Empty line: SSE event separator, skip.
		if line == "" {
			continue
		}

		// SSE comment (starts with ":"): skip.
		if strings.HasPrefix(line, ":") {
			continue
		}

		// Must be a "data:" line.
		if !strings.HasPrefix(line, "data: ") && !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		data = strings.TrimPrefix(data, "data:")
		data = strings.TrimSpace(data)

		// [DONE] sentinel: stream is complete.
		if data == "[DONE]" {
			return
		}

		// OpenRouter keepalive: "data: : OPENROUTER..."
		if strings.HasPrefix(data, ": OPENROUTER") || strings.HasPrefix(data, ":OPENROUTER") {
			continue
		}

		// Parse the JSON chunk.
		var chunk apiStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			ch <- provider.StreamChunk{
				Err: err,
			}
			return
		}

		// Check for API error in chunk.
		if chunk.Error.Message != "" {
			ch <- provider.StreamChunk{
				Err: mapAPIError(apiError{Error: chunk.Error}),
			}
			return
		}

		// No choices means nothing to emit.
		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		delta := choice.Delta

		// Build the stream chunk.
		sc := provider.StreamChunk{
			Content: delta.Content,
		}

		// Accumulate tool calls from deltas.
		if len(delta.ToolCalls) > 0 {
			if toolArgs == nil {
				toolArgs = make(map[int]*toolCallAccumulator)
			}
			for _, tc := range delta.ToolCalls {
				acc, ok := toolArgs[tc.Index]
				if !ok {
					acc = &toolCallAccumulator{
						id:   tc.ID,
						name: tc.Function.Name,
					}
					toolArgs[tc.Index] = acc
				}
				if tc.ID != "" {
					acc.id = tc.ID
				}
				if tc.Function.Name != "" {
					acc.name = tc.Function.Name
				}
				acc.args += tc.Function.Arguments
			}
		}

		// Map finish reason.
		if choice.FinishReason != "" {
			sc.FinishReason = mapFinishReason(choice.FinishReason)

			// Emit accumulated tool calls on the final chunk.
			if toolArgs != nil {
				sc.ToolCalls = buildToolCalls(toolArgs)
			}
		}

		// Attach usage if present.
		if chunk.Usage != nil {
			sc.Usage = &provider.TokenUsage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
		}

		ch <- sc
	}

	// Scanner error (e.g., connection reset).
	if err := scanner.Err(); err != nil {
		ch <- provider.StreamChunk{Err: err}
	}
}

// toolCallAccumulator collects streamed fragments of a single tool call.
type toolCallAccumulator struct {
	id   string
	name string
	args string
}

// buildToolCalls converts accumulated tool call fragments into provider.ToolCalls.
// Indexes are sorted to produce a deterministic order even when sparse.
func buildToolCalls(accs map[int]*toolCallAccumulator) []provider.ToolCall {
	keys := slices.Sorted(maps.Keys(accs))
	calls := make([]provider.ToolCall, 0, len(keys))
	for _, idx := range keys {
		acc := accs[idx]
		calls = append(calls, provider.ToolCall{
			ID:        acc.id,
			Name:      acc.name,
			Arguments: json.RawMessage(acc.args),
		})
	}
	return calls
}

// mapFinishReason converts an OpenAI-compatible finish_reason string
// to a provider.FinishReason.
func mapFinishReason(reason string) provider.FinishReason {
	switch reason {
	case "stop":
		return provider.FinishReasonStop
	case "length":
		return provider.FinishReasonLength
	case "tool_calls":
		return provider.FinishReasonToolUse
	case "content_filter":
		return provider.FinishReasonFiltering
	default:
		return provider.FinishReasonStop
	}
}
