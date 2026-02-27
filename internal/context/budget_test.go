package ctxengine_test

import (
	"encoding/json"
	"testing"

	ctxengine "github.com/flemzord/sclaw/internal/context"
	"github.com/flemzord/sclaw/internal/provider"
)

// Compile-time interface guard: CharEstimator must satisfy TokenEstimator.
var _ ctxengine.TokenEstimator = (*ctxengine.CharEstimator)(nil)

// ---------------------------------------------------------------------------
// NewCharEstimator
// ---------------------------------------------------------------------------

func TestNewCharEstimator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		charsPerToken float64
		wantRatio     float64
	}{
		{name: "valid_ratio", charsPerToken: 3.0, wantRatio: 3.0},
		{name: "zero_defaults_to_4", charsPerToken: 0, wantRatio: 4.0},
		{name: "negative_defaults_to_4", charsPerToken: -1.5, wantRatio: 4.0},
		{name: "large_ratio", charsPerToken: 10.0, wantRatio: 10.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			est := ctxengine.NewCharEstimator(tt.charsPerToken)
			if est.CharsPerToken != tt.wantRatio {
				t.Errorf("NewCharEstimator(%v).CharsPerToken = %v, want %v",
					tt.charsPerToken, est.CharsPerToken, tt.wantRatio)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CharEstimator.Estimate
// ---------------------------------------------------------------------------

func TestCharEstimator_Estimate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		charsPerToken float64 // 0 means default (4.0)
		input         string
		want          int
	}{
		// Default ratio (0 → uses 4.0)
		{name: "default_empty", charsPerToken: 0, input: "", want: 0},
		{name: "default_single_char", charsPerToken: 0, input: "a", want: 1},
		{name: "default_hello", charsPerToken: 0, input: "hello", want: 2},
		{name: "default_hello_world_bang", charsPerToken: 0, input: "hello world!!", want: 4},
		{name: "default_exact_multiple", charsPerToken: 0, input: "abcd", want: 2}, // int(4/4)+1 = 2
		// Custom ratio 3.0
		{name: "custom3_hello_world", charsPerToken: 3.0, input: "hello world", want: 4}, // int(11/3)+1 = 4
		{name: "custom3_empty", charsPerToken: 3.0, input: "", want: 0},
		// Negative ratio defaults to 4.0
		{name: "negative_ratio_hello", charsPerToken: -2.0, input: "hello", want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			est := ctxengine.NewCharEstimator(tt.charsPerToken)
			got := est.Estimate(tt.input)
			if got != tt.want {
				t.Errorf("Estimate(%q) = %d, want %d (ratio=%v)", tt.input, got, tt.want, est.CharsPerToken)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ContextBudget.Used
// ---------------------------------------------------------------------------

func TestContextBudget_Used(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		budget ctxengine.ContextBudget
		want   int
	}{
		{
			name:   "all_zero",
			budget: ctxengine.ContextBudget{},
			want:   0,
		},
		{
			name: "sum_of_sections",
			budget: ctxengine.ContextBudget{
				WindowSize: 5000,
				System:     100,
				Tools:      50,
				Memory:     30,
				History:    200,
				Reserved:   100,
			},
			want: 480,
		},
		{
			name: "single_section",
			budget: ctxengine.ContextBudget{
				System: 42,
			},
			want: 42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.budget.Used()
			if got != tt.want {
				t.Errorf("Used() = %d, want %d", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ContextBudget.Available
// ---------------------------------------------------------------------------

func TestContextBudget_Available(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		budget ctxengine.ContextBudget
		want   int
	}{
		{
			name: "normal_remaining",
			budget: ctxengine.ContextBudget{
				WindowSize: 1000,
				System:     100,
				Tools:      50,
				Memory:     30,
				History:    200,
				Reserved:   100,
			},
			// Used = 480, Available = 520
			want: 520,
		},
		{
			name: "exceeded_returns_zero",
			budget: ctxengine.ContextBudget{
				WindowSize: 100,
				System:     50,
				Tools:      30,
				Memory:     10,
				History:    20,
				Reserved:   10,
			},
			// Used = 120 > 100 → 0
			want: 0,
		},
		{
			name: "exactly_at_limit",
			budget: ctxengine.ContextBudget{
				WindowSize: 100,
				System:     40,
				Tools:      30,
				Memory:     10,
				History:    10,
				Reserved:   10,
			},
			// Used = 100 == 100 → 0
			want: 0,
		},
		{
			name:   "zero_window",
			budget: ctxengine.ContextBudget{},
			want:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.budget.Available()
			if got != tt.want {
				t.Errorf("Available() = %d, want %d (Used=%d, Window=%d)",
					got, tt.want, tt.budget.Used(), tt.budget.WindowSize)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ContextBudget.Exceeded
// ---------------------------------------------------------------------------

func TestContextBudget_Exceeded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		budget ctxengine.ContextBudget
		want   bool
	}{
		{
			name: "exceeded",
			budget: ctxengine.ContextBudget{
				WindowSize: 100,
				System:     50,
				Tools:      30,
				Memory:     10,
				History:    20,
				Reserved:   10,
			},
			// Used = 120 > 100
			want: true,
		},
		{
			name: "not_exceeded",
			budget: ctxengine.ContextBudget{
				WindowSize: 1000,
				System:     100,
				Tools:      50,
				Memory:     30,
				History:    200,
				Reserved:   100,
			},
			// Used = 480 < 1000
			want: false,
		},
		{
			name: "exactly_at_limit_not_exceeded",
			budget: ctxengine.ContextBudget{
				WindowSize: 100,
				System:     40,
				Tools:      30,
				Memory:     10,
				History:    10,
				Reserved:   10,
			},
			// Used = 100 == 100 → not exceeded (> not >=)
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.budget.Exceeded()
			if got != tt.want {
				t.Errorf("Exceeded() = %v, want %v (Used=%d, WindowSize=%d)",
					got, tt.want, tt.budget.Used(), tt.budget.WindowSize)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// EstimateMessages
// ---------------------------------------------------------------------------

func TestEstimateMessages(t *testing.T) {
	t.Parallel()

	est := ctxengine.NewCharEstimator(0) // default ratio 4.0

	tests := []struct {
		name     string
		messages []provider.LLMMessage
		want     int
	}{
		{
			name:     "empty_slice",
			messages: nil,
			want:     0,
		},
		{
			name: "single_message_hello",
			messages: []provider.LLMMessage{
				{Role: provider.MessageRoleUser, Content: "hello"},
			},
			// perMessageOverhead(4) + ceil(5/4)=2 = 6
			want: 6,
		},
		{
			name: "message_with_name",
			messages: []provider.LLMMessage{
				{Role: provider.MessageRoleTool, Content: "result", Name: "search"},
			},
			// overhead(4) + Estimate("result"=6 → int(6/4)+1=2) + Estimate("search"=6 → 2) = 8
			want: 8,
		},
		{
			name: "message_with_tool_calls",
			messages: []provider.LLMMessage{
				{
					Role:    provider.MessageRoleAssistant,
					Content: "calling tool",
					ToolCalls: []provider.ToolCall{
						{
							ID:        "tc1",
							Name:      "search",
							Arguments: json.RawMessage(`{"query":"test"}`),
						},
					},
				},
			},
			// overhead(4) + Estimate("calling tool"=12 → int(12/4)+1=4)
			// + Estimate("search"=6 → int(6/4)+1=2)
			// + Estimate(`{"query":"test"}`=16 → int(16/4)+1=5)
			// = 4 + 4 + 2 + 5 = 15
			want: 15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ctxengine.EstimateMessages(est, tt.messages)
			if got != tt.want {
				t.Errorf("EstimateMessages() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestEstimateMessages_WithImageParts(t *testing.T) {
	t.Parallel()

	est := ctxengine.NewCharEstimator(0) // default ratio 4.0

	messages := []provider.LLMMessage{
		{
			Role: provider.MessageRoleUser,
			ContentParts: []provider.ContentPart{
				{Type: provider.ContentPartImageURL, ImageURL: &provider.ImageURL{URL: "https://example.com/photo.jpg"}},
				{Type: provider.ContentPartText, Text: "What is this?"},
			},
		},
	}

	got := ctxengine.EstimateMessages(est, messages)

	// overhead(4) + image(765) + text("What is this?"=14 → int(14/4)+1=4) = 773
	want := 4 + 765 + 4
	if got != want {
		t.Errorf("EstimateMessages(image+text) = %d, want %d", got, want)
	}
}

func TestEstimateMessages_MultipleMessages(t *testing.T) {
	t.Parallel()

	est := ctxengine.NewCharEstimator(0)

	messages := []provider.LLMMessage{
		{Role: provider.MessageRoleSystem, Content: "You are helpful."},
		{Role: provider.MessageRoleUser, Content: "Hello!"},
		{Role: provider.MessageRoleAssistant, Content: "Hi there!"},
	}

	got := ctxengine.EstimateMessages(est, messages)
	if got <= 0 {
		t.Errorf("EstimateMessages() = %d, want > 0", got)
	}

	// Each message has at least 4 tokens overhead.
	minExpected := len(messages) * 4
	if got < minExpected {
		t.Errorf("EstimateMessages() = %d, want >= %d (per-message overhead)", got, minExpected)
	}
}

// ---------------------------------------------------------------------------
// EstimateToolDefinitions
// ---------------------------------------------------------------------------

func TestEstimateToolDefinitions(t *testing.T) {
	t.Parallel()

	est := ctxengine.NewCharEstimator(0)

	tests := []struct {
		name  string
		tools []provider.ToolDefinition
	}{
		{
			name:  "empty_slice",
			tools: nil,
		},
		{
			name: "with_tools",
			tools: []provider.ToolDefinition{
				{
					Name:        "search",
					Description: "Search the web for information",
					Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
				},
				{
					Name:        "calc",
					Description: "Evaluate a math expression",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ctxengine.EstimateToolDefinitions(est, tt.tools)
			if len(tt.tools) == 0 {
				if got != 0 {
					t.Errorf("EstimateToolDefinitions(empty) = %d, want 0", got)
				}
			} else {
				if got <= 0 {
					t.Errorf("EstimateToolDefinitions() = %d, want > 0", got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// EstimateTools (per-field estimation)
// ---------------------------------------------------------------------------

func TestEstimateTools(t *testing.T) {
	t.Parallel()

	est := ctxengine.NewCharEstimator(0)

	tools := []provider.ToolDefinition{
		{
			Name:        "search",
			Description: "Search the web for information",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
		},
		{
			Name:        "calc",
			Description: "Evaluate a math expression",
		},
	}

	got := ctxengine.EstimateTools(est, tools)
	if got <= 0 {
		t.Errorf("EstimateTools() = %d, want > 0", got)
	}

	// Verify it accounts for name + description + params of each tool.
	nameOnly := est.Estimate("search") + est.Estimate("calc")
	if got < nameOnly {
		t.Errorf("EstimateTools() = %d, want >= %d (at least name tokens)", got, nameOnly)
	}
}

// ---------------------------------------------------------------------------
// EstimateSystemPrompt
// ---------------------------------------------------------------------------

func TestEstimateSystemPrompt(t *testing.T) {
	t.Parallel()

	est := ctxengine.NewCharEstimator(0)

	tests := []struct {
		name  string
		parts []string
		want  int // 0 means "check > 0"
	}{
		{
			name:  "empty_parts",
			parts: nil,
			want:  0, // empty string → 0 tokens
		},
		{
			name:  "single_part",
			parts: []string{"You are helpful"},
		},
		{
			name:  "multiple_parts",
			parts: []string{"You are helpful", "Be concise", "Use tools when needed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ctxengine.EstimateSystemPrompt(est, tt.parts)
			if tt.want == 0 && len(tt.parts) == 0 {
				if got != 0 {
					t.Errorf("EstimateSystemPrompt(empty) = %d, want 0", got)
				}
			} else if got <= 0 {
				t.Errorf("EstimateSystemPrompt() = %d, want > 0", got)
			}
		})
	}
}
