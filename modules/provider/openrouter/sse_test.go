package openrouter

import (
	"errors"
	"strings"
	"testing"

	"github.com/flemzord/sclaw/internal/provider"
)

// errAny is a sentinel used in tests where we only want to assert an error
// is non-nil without checking its wrapping chain.
var errAny = errors.New("any")

func TestParseSSE(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []provider.StreamChunk
	}{
		{
			name: "simple text chunks",
			input: `data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":""}]}

data: {"choices":[{"delta":{"content":" world"},"finish_reason":"stop"}]}

data: [DONE]
`,
			want: []provider.StreamChunk{
				{Content: "Hello"},
				{Content: " world", FinishReason: provider.FinishReasonStop},
			},
		},
		{
			name: "keepalive filtered",
			input: `data: : OPENROUTER PROCESSING

data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}

data: [DONE]
`,
			want: []provider.StreamChunk{
				{Content: "ok", FinishReason: provider.FinishReasonStop},
			},
		},
		{
			name: "SSE comments filtered",
			input: `: this is a comment
data: {"choices":[{"delta":{"content":"hi"},"finish_reason":"stop"}]}

data: [DONE]
`,
			want: []provider.StreamChunk{
				{Content: "hi", FinishReason: provider.FinishReasonStop},
			},
		},
		{
			name:  "empty stream",
			input: "data: [DONE]\n",
			want:  nil,
		},
		{
			name: "mid-stream error",
			input: `data: {"choices":[{"delta":{"content":"partial"},"finish_reason":""}]}

data: {"error":{"message":"rate limit exceeded","code":429}}
`,
			want: []provider.StreamChunk{
				{Content: "partial"},
				{Err: provider.ErrRateLimit}, // checked via errors.Is
			},
		},
		{
			name: "tool calls streamed",
			input: `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":""}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]},"finish_reason":""}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Paris\"}"}}]},"finish_reason":"tool_calls"}]}

data: [DONE]
`,
			want: []provider.StreamChunk{
				{},
				{},
				{
					FinishReason: provider.FinishReasonToolUse,
					ToolCalls: []provider.ToolCall{
						{
							ID:        "call_1",
							Name:      "get_weather",
							Arguments: []byte(`{"city":"Paris"}`),
						},
					},
				},
			},
		},
		{
			name: "finish reason length",
			input: `data: {"choices":[{"delta":{"content":"truncated"},"finish_reason":"length"}]}

data: [DONE]
`,
			want: []provider.StreamChunk{
				{Content: "truncated", FinishReason: provider.FinishReasonLength},
			},
		},
		{
			name: "finish reason content_filter",
			input: `data: {"choices":[{"delta":{"content":""},"finish_reason":"content_filter"}]}

data: [DONE]
`,
			want: []provider.StreamChunk{
				{FinishReason: provider.FinishReasonFiltering},
			},
		},
		{
			name:  "malformed JSON",
			input: "data: {not valid json}\n",
			want: []provider.StreamChunk{
				{Err: errAny}, // just check error is non-nil
			},
		},
		{
			name: "mid-stream context length error",
			input: `data: {"error":{"message":"context length exceeded","code":400}}
`,
			want: []provider.StreamChunk{
				{Err: provider.ErrContextLength},
			},
		},
		{
			name: "mid-stream generic error",
			input: `data: {"error":{"message":"something broke","code":500}}
`,
			want: []provider.StreamChunk{
				{Err: provider.ErrProviderDown},
			},
		},
		{
			name: "usage in final chunk",
			input: `data: {"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}

data: [DONE]
`,
			want: []provider.StreamChunk{
				{
					Content:      "done",
					FinishReason: provider.FinishReasonStop,
					Usage: &provider.TokenUsage{
						PromptTokens:     10,
						CompletionTokens: 5,
						TotalTokens:      15,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ch := make(chan provider.StreamChunk, 32)
			r := strings.NewReader(tt.input)

			go func() {
				defer close(ch)
				parseSSE(r, ch)
			}()

			var got []provider.StreamChunk
			for chunk := range ch {
				got = append(got, chunk)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("got %d chunks, want %d", len(got), len(tt.want))
			}

			for i, want := range tt.want {
				g := got[i]

				// Check error chunks via errors.Is.
				if want.Err != nil {
					if g.Err == nil {
						t.Errorf("chunk[%d]: expected error", i)
					} else if !errors.Is(want.Err, errAny) && !errors.Is(g.Err, want.Err) {
						t.Errorf("chunk[%d]: error = %v, want wrapping %v", i, g.Err, want.Err)
					}
					continue
				}
				if g.Err != nil {
					t.Errorf("chunk[%d]: unexpected error: %v", i, g.Err)
					continue
				}

				if g.Content != want.Content {
					t.Errorf("chunk[%d].Content = %q, want %q", i, g.Content, want.Content)
				}
				if g.FinishReason != want.FinishReason {
					t.Errorf("chunk[%d].FinishReason = %q, want %q", i, g.FinishReason, want.FinishReason)
				}

				// Check tool calls.
				if len(g.ToolCalls) != len(want.ToolCalls) {
					t.Errorf("chunk[%d]: got %d tool calls, want %d", i, len(g.ToolCalls), len(want.ToolCalls))
					continue
				}
				for j, wtc := range want.ToolCalls {
					gtc := g.ToolCalls[j]
					if gtc.ID != wtc.ID {
						t.Errorf("chunk[%d].ToolCalls[%d].ID = %q, want %q", i, j, gtc.ID, wtc.ID)
					}
					if gtc.Name != wtc.Name {
						t.Errorf("chunk[%d].ToolCalls[%d].Name = %q, want %q", i, j, gtc.Name, wtc.Name)
					}
					if string(gtc.Arguments) != string(wtc.Arguments) {
						t.Errorf("chunk[%d].ToolCalls[%d].Arguments = %s, want %s", i, j, gtc.Arguments, wtc.Arguments)
					}
				}

				// Check usage.
				if want.Usage != nil {
					if g.Usage == nil {
						t.Errorf("chunk[%d]: expected usage", i)
					} else if *g.Usage != *want.Usage {
						t.Errorf("chunk[%d].Usage = %+v, want %+v", i, *g.Usage, *want.Usage)
					}
				}
			}
		})
	}
}
