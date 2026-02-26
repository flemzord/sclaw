package openaicompat

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/flemzord/sclaw/internal/provider"
)

// oaiStreamChunk represents a single SSE chunk from the OpenAI streaming API.
type oaiStreamChunk struct {
	Choices []oaiStreamChoice `json:"choices"`
	Usage   *oaiUsage         `json:"usage,omitempty"`
}

type oaiStreamChoice struct {
	Delta        oaiStreamDelta `json:"delta"`
	FinishReason *string        `json:"finish_reason"`
}

type oaiStreamDelta struct {
	Content   string          `json:"content,omitempty"`
	ToolCalls []oaiStreamTool `json:"tool_calls,omitempty"`
}

type oaiStreamTool struct {
	Index    int             `json:"index"`
	ID       string          `json:"id,omitempty"`
	Type     string          `json:"type,omitempty"`
	Function oaiToolFunction `json:"function"`
}

// toolAccumulator collects streaming tool call deltas by index.
type toolAccumulator struct {
	calls map[int]*provider.ToolCall
}

func newToolAccumulator() *toolAccumulator {
	return &toolAccumulator{calls: make(map[int]*provider.ToolCall)}
}

// add merges a streaming tool call delta into the accumulator.
func (ta *toolAccumulator) add(st oaiStreamTool) {
	tc, ok := ta.calls[st.Index]
	if !ok {
		tc = &provider.ToolCall{}
		ta.calls[st.Index] = tc
	}
	if st.ID != "" {
		tc.ID = st.ID
	}
	if st.Function.Name != "" {
		tc.Name = st.Function.Name
	}
	if st.Function.Arguments != "" {
		tc.Arguments = json.RawMessage(
			string(tc.Arguments) + st.Function.Arguments,
		)
	}
}

// result returns the accumulated tool calls in index order.
func (ta *toolAccumulator) result() []provider.ToolCall {
	if len(ta.calls) == 0 {
		return nil
	}
	// Find max index for ordering.
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

// parseSSEStream reads an SSE response body and emits StreamChunks on the returned channel.
// The channel is closed when the stream ends, either by [DONE] or an error.
// Context cancellation is respected.
func (p *Provider) parseSSEStream(ctx context.Context, scanner *bufio.Scanner) <-chan provider.StreamChunk {
	ch := make(chan provider.StreamChunk, 16)

	go func() {
		defer close(ch)

		tools := newToolAccumulator()

		for scanner.Scan() {
			if err := ctx.Err(); err != nil {
				ch <- provider.StreamChunk{Err: err}
				return
			}

			line := scanner.Text()

			// SSE format: accept both "data: " (with space) and "data:" (without).
			// Some OpenAI-compatible providers omit the space after the colon.
			var data string
			switch {
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			case strings.HasPrefix(line, "data:"):
				data = strings.TrimPrefix(line, "data:")
			default:
				continue
			}

			// End of stream.
			if data == "[DONE]" {
				// Emit final tool calls if accumulated.
				if tcs := tools.result(); len(tcs) > 0 {
					ch <- provider.StreamChunk{ToolCalls: tcs}
				}
				return
			}

			var chunk oaiStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				ch <- provider.StreamChunk{
					Err: fmt.Errorf("parse SSE chunk: %w", err),
				}
				return
			}

			sc := provider.StreamChunk{}

			if chunk.Usage != nil {
				sc.Usage = &provider.TokenUsage{
					PromptTokens:     chunk.Usage.PromptTokens,
					CompletionTokens: chunk.Usage.CompletionTokens,
					TotalTokens:      chunk.Usage.TotalTokens,
				}
			}

			if len(chunk.Choices) > 0 {
				choice := chunk.Choices[0]

				if choice.Delta.Content != "" {
					sc.Content = choice.Delta.Content
				}

				for _, tc := range choice.Delta.ToolCalls {
					tools.add(tc)
				}

				if choice.FinishReason != nil {
					sc.FinishReason = mapFinishReason(*choice.FinishReason)
				}
			}

			// Only emit if there is actual content or a finish reason.
			if sc.Content != "" || sc.FinishReason != "" || sc.Usage != nil {
				ch <- sc
			}
		}

		// Scanner error (connection drop, etc.)
		if err := scanner.Err(); err != nil {
			// Do not classify context cancellation as provider failure.
			if ctx.Err() != nil {
				ch <- provider.StreamChunk{Err: ctx.Err()}
			} else {
				ch <- provider.StreamChunk{
					Err: fmt.Errorf("%w: stream read error: %w", provider.ErrProviderDown, err),
				}
			}
		}
	}()

	return ch
}
