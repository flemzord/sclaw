package openai

import (
	"encoding/json"
	"testing"

	"github.com/flemzord/sclaw/internal/provider"
)

func TestToMessages_AllRoles(t *testing.T) {
	msgs := []provider.LLMMessage{
		{Role: provider.MessageRoleSystem, Content: "You are helpful."},
		{Role: provider.MessageRoleUser, Content: "Hello", Name: "alice"},
		{Role: provider.MessageRoleAssistant, Content: "Hi!", ToolCalls: []provider.ToolCall{
			{ID: "call_1", Name: "search", Arguments: json.RawMessage(`{"q":"test"}`)},
		}},
		{Role: provider.MessageRoleTool, Content: `{"result":"ok"}`, ToolID: "call_1"},
	}

	out := toMessages(msgs)

	if len(out) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(out))
	}

	// System
	if out[0].Role != "system" || out[0].Content != "You are helpful." {
		t.Errorf("system message mismatch: %+v", out[0])
	}

	// User with name
	if out[1].Role != "user" || out[1].Name != "alice" {
		t.Errorf("user message mismatch: %+v", out[1])
	}

	// Assistant with tool calls
	if out[2].Role != "assistant" || len(out[2].ToolCalls) != 1 {
		t.Errorf("assistant message mismatch: %+v", out[2])
	}
	tc := out[2].ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "search" || tc.Type != "function" {
		t.Errorf("tool call mismatch: %+v", tc)
	}
	if tc.Function.Arguments != `{"q":"test"}` {
		t.Errorf("arguments = %q, want {\"q\":\"test\"}", tc.Function.Arguments)
	}

	// Tool result
	if out[3].Role != "tool" || out[3].ToolCallID != "call_1" {
		t.Errorf("tool message mismatch: %+v", out[3])
	}
}

func TestToTools(t *testing.T) {
	tools := []provider.ToolDefinition{
		{
			Name:        "search",
			Description: "Search the web",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
		},
	}

	out := toTools(tools)

	if len(out) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(out))
	}
	if out[0].Type != "function" {
		t.Errorf("type = %q, want function", out[0].Type)
	}
	if out[0].Function.Name != "search" {
		t.Errorf("name = %q, want search", out[0].Function.Name)
	}
	if out[0].Function.Description != "Search the web" {
		t.Errorf("description = %q, want 'Search the web'", out[0].Function.Description)
	}
}

func TestFromResponse(t *testing.T) {
	stop := "stop"
	resp := &chatResponse{
		Choices: []chatChoice{
			{
				Message: chatMessage{
					Role:    "assistant",
					Content: "Hello!",
				},
				FinishReason: &stop,
			},
		},
		Usage: chatUsage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	cr := fromResponse(resp)

	if cr.Content != "Hello!" {
		t.Errorf("content = %q, want Hello!", cr.Content)
	}
	if cr.FinishReason != provider.FinishReasonStop {
		t.Errorf("finish_reason = %q, want stop", cr.FinishReason)
	}
	if cr.Usage.TotalTokens != 15 {
		t.Errorf("total_tokens = %d, want 15", cr.Usage.TotalTokens)
	}
}

func TestFromToolCalls(t *testing.T) {
	calls := []chatToolCall{
		{
			ID:   "call_abc",
			Type: "function",
			Function: chatFunctionCall{
				Name:      "get_weather",
				Arguments: `{"city":"Paris"}`,
			},
		},
	}

	out := fromToolCalls(calls)

	if len(out) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(out))
	}
	if out[0].ID != "call_abc" {
		t.Errorf("id = %q, want call_abc", out[0].ID)
	}
	if out[0].Name != "get_weather" {
		t.Errorf("name = %q, want get_weather", out[0].Name)
	}
	if string(out[0].Arguments) != `{"city":"Paris"}` {
		t.Errorf("arguments = %s, want {\"city\":\"Paris\"}", out[0].Arguments)
	}
}

func TestFromToolCalls_Empty(t *testing.T) {
	out := fromToolCalls(nil)
	if out != nil {
		t.Errorf("expected nil for empty tool calls, got %v", out)
	}
}

func TestMapFinishReason(t *testing.T) {
	tests := []struct {
		input *string
		want  provider.FinishReason
	}{
		{strPtr("stop"), provider.FinishReasonStop},
		{strPtr("length"), provider.FinishReasonLength},
		{strPtr("tool_calls"), provider.FinishReasonToolUse},
		{strPtr("content_filter"), provider.FinishReasonFiltering},
		{strPtr("unknown_reason"), provider.FinishReason("unknown_reason")},
		{nil, ""},
	}

	for _, tt := range tests {
		name := "nil"
		if tt.input != nil {
			name = *tt.input
		}
		t.Run(name, func(t *testing.T) {
			got := mapFinishReason(tt.input)
			if got != tt.want {
				t.Errorf("mapFinishReason(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func strPtr(s string) *string { return &s }
