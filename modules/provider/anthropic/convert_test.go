package anthropic

import (
	"encoding/json"
	"testing"

	sdkanthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/flemzord/sclaw/internal/provider"
)

func TestSplitSystemMessages_LeadingSystem(t *testing.T) {
	msgs := []provider.LLMMessage{
		{Role: provider.MessageRoleSystem, Content: "You are helpful."},
		{Role: provider.MessageRoleSystem, Content: "Be concise."},
		{Role: provider.MessageRoleUser, Content: "Hello"},
	}

	system, rest := splitSystemMessages(msgs)

	if len(system) != 2 {
		t.Fatalf("expected 2 system blocks, got %d", len(system))
	}
	if system[0].Text != "You are helpful." {
		t.Errorf("expected first system text 'You are helpful.', got %q", system[0].Text)
	}
	if system[1].Text != "Be concise." {
		t.Errorf("expected second system text 'Be concise.', got %q", system[1].Text)
	}
	if len(rest) != 1 {
		t.Fatalf("expected 1 remaining message, got %d", len(rest))
	}
	if rest[0].Role != provider.MessageRoleUser {
		t.Errorf("expected remaining message role 'user', got %q", rest[0].Role)
	}
}

func TestSplitSystemMessages_NoSystem(t *testing.T) {
	msgs := []provider.LLMMessage{
		{Role: provider.MessageRoleUser, Content: "Hello"},
	}

	system, rest := splitSystemMessages(msgs)

	if len(system) != 0 {
		t.Fatalf("expected 0 system blocks, got %d", len(system))
	}
	if len(rest) != 1 {
		t.Fatalf("expected 1 remaining message, got %d", len(rest))
	}
}

func TestSplitSystemMessages_AllSystem(t *testing.T) {
	msgs := []provider.LLMMessage{
		{Role: provider.MessageRoleSystem, Content: "System only"},
	}

	system, rest := splitSystemMessages(msgs)

	if len(system) != 1 {
		t.Fatalf("expected 1 system block, got %d", len(system))
	}
	if len(rest) != 0 {
		t.Fatalf("expected 0 remaining messages, got %d", len(rest))
	}
}

func TestConvertMessages_UserAndAssistant(t *testing.T) {
	msgs := []provider.LLMMessage{
		{Role: provider.MessageRoleUser, Content: "Hello"},
		{Role: provider.MessageRoleAssistant, Content: "Hi there"},
		{Role: provider.MessageRoleUser, Content: "How are you?"},
	}

	result := convertMessages(msgs, nil)

	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	if result[0].Role != sdkanthropic.MessageParamRoleUser {
		t.Errorf("expected first message role 'user', got %q", result[0].Role)
	}
	if result[1].Role != sdkanthropic.MessageParamRoleAssistant {
		t.Errorf("expected second message role 'assistant', got %q", result[1].Role)
	}
}

func TestConvertMessages_ToolResultGrouping(t *testing.T) {
	msgs := []provider.LLMMessage{
		{Role: provider.MessageRoleUser, Content: "Use tools"},
		{Role: provider.MessageRoleAssistant, Content: "Sure", ToolCalls: []provider.ToolCall{
			{ID: "tc1", Name: "tool_a", Arguments: json.RawMessage(`{"x":1}`)},
			{ID: "tc2", Name: "tool_b", Arguments: json.RawMessage(`{"y":2}`)},
		}},
		{Role: provider.MessageRoleTool, ToolID: "tc1", Content: "result_a"},
		{Role: provider.MessageRoleTool, ToolID: "tc2", Content: "result_b"},
	}

	result := convertMessages(msgs, nil)

	// user + assistant + 1 grouped user (tool results) = 3
	if len(result) != 3 {
		t.Fatalf("expected 3 messages (tool results grouped), got %d", len(result))
	}

	// The last message should be a user message with 2 tool_result blocks.
	lastMsg := result[2]
	if lastMsg.Role != sdkanthropic.MessageParamRoleUser {
		t.Errorf("expected grouped tool result message role 'user', got %q", lastMsg.Role)
	}
	if len(lastMsg.Content) != 2 {
		t.Fatalf("expected 2 content blocks in grouped tool result, got %d", len(lastMsg.Content))
	}
}

func TestConvertMessages_AssistantWithToolCalls(t *testing.T) {
	msgs := []provider.LLMMessage{
		{Role: provider.MessageRoleAssistant, Content: "Let me check", ToolCalls: []provider.ToolCall{
			{ID: "tc1", Name: "search", Arguments: json.RawMessage(`{"q":"test"}`)},
		}},
	}

	result := convertMessages(msgs, nil)

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	// Should have text block + tool_use block = 2 content blocks.
	if len(result[0].Content) != 2 {
		t.Fatalf("expected 2 content blocks (text + tool_use), got %d", len(result[0].Content))
	}
}

func TestConvertMessages_NonLeadingSystemDropped(t *testing.T) {
	msgs := []provider.LLMMessage{
		{Role: provider.MessageRoleUser, Content: "Hello"},
		{Role: provider.MessageRoleSystem, Content: "This should be dropped"},
		{Role: provider.MessageRoleUser, Content: "World"},
	}

	result := convertMessages(msgs, nil)

	// Only user messages should survive; system message at index 1 is dropped.
	if len(result) != 2 {
		t.Fatalf("expected 2 messages (non-leading system dropped), got %d", len(result))
	}
}

func TestConvertResponse_TextOnly(t *testing.T) {
	msg := &sdkanthropic.Message{
		Content: []sdkanthropic.ContentBlockUnion{
			textBlock("Hello world"),
		},
		StopReason: sdkanthropic.StopReasonEndTurn,
		Usage: sdkanthropic.Usage{
			InputTokens:  10,
			OutputTokens: 5,
		},
	}

	resp := convertResponse(msg)

	if resp.Content != "Hello world" {
		t.Errorf("expected content 'Hello world', got %q", resp.Content)
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(resp.ToolCalls))
	}
	if resp.FinishReason != provider.FinishReasonStop {
		t.Errorf("expected finish reason 'stop', got %q", resp.FinishReason)
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("expected prompt tokens 10, got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 5 {
		t.Errorf("expected completion tokens 5, got %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("expected total tokens 15, got %d", resp.Usage.TotalTokens)
	}
}

func TestConvertResponse_ToolUse(t *testing.T) {
	msg := &sdkanthropic.Message{
		Content: []sdkanthropic.ContentBlockUnion{
			toolUseBlock("tc1", "get_weather", `{"city":"Paris"}`),
		},
		StopReason: sdkanthropic.StopReasonToolUse,
		Usage: sdkanthropic.Usage{
			InputTokens:  20,
			OutputTokens: 10,
		},
	}

	resp := convertResponse(msg)

	if resp.Content != "" {
		t.Errorf("expected empty content, got %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "tc1" {
		t.Errorf("expected tool call ID 'tc1', got %q", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("expected tool call name 'get_weather', got %q", tc.Name)
	}
	if resp.FinishReason != provider.FinishReasonToolUse {
		t.Errorf("expected finish reason 'tool_use', got %q", resp.FinishReason)
	}
}

func TestConvertResponse_Mixed(t *testing.T) {
	msg := &sdkanthropic.Message{
		Content: []sdkanthropic.ContentBlockUnion{
			textBlock("I'll search for that"),
			toolUseBlock("tc1", "search", `{"q":"test"}`),
		},
		StopReason: sdkanthropic.StopReasonToolUse,
		Usage:      sdkanthropic.Usage{InputTokens: 15, OutputTokens: 8},
	}

	resp := convertResponse(msg)

	if resp.Content != "I'll search for that" {
		t.Errorf("expected content text, got %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
}

func TestConvertStopReason(t *testing.T) {
	tests := []struct {
		input    sdkanthropic.StopReason
		expected provider.FinishReason
	}{
		{sdkanthropic.StopReasonEndTurn, provider.FinishReasonStop},
		{sdkanthropic.StopReasonStopSequence, provider.FinishReasonStop},
		{sdkanthropic.StopReasonMaxTokens, provider.FinishReasonLength},
		{sdkanthropic.StopReasonToolUse, provider.FinishReasonToolUse},
		{sdkanthropic.StopReasonRefusal, provider.FinishReasonFiltering},
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			got := convertStopReason(tt.input)
			if got != tt.expected {
				t.Errorf("convertStopReason(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestConvertTools(t *testing.T) {
	tools := []provider.ToolDefinition{
		{
			Name:        "get_weather",
			Description: "Get weather for a city",
			Parameters:  json.RawMessage(`{"properties":{"city":{"type":"string"}},"required":["city"]}`),
		},
	}

	result := convertTools(tools)

	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}
	tp := result[0].OfTool
	if tp == nil {
		t.Fatal("expected OfTool to be non-nil")
	}
	if tp.Name != "get_weather" {
		t.Errorf("expected tool name 'get_weather', got %q", tp.Name)
	}
	if tp.InputSchema.Required[0] != "city" {
		t.Errorf("expected required field 'city', got %q", tp.InputSchema.Required[0])
	}
}

func TestConvertInputSchema_PreservesExtraFields(t *testing.T) {
	raw := json.RawMessage(`{
		"properties": {"name": {"type": "string"}},
		"required": ["name"],
		"additionalProperties": false,
		"$defs": {"Foo": {"type": "object"}}
	}`)

	schema := convertInputSchema(raw)

	if schema.Properties == nil {
		t.Fatal("expected properties to be set")
	}
	if len(schema.Required) != 1 || schema.Required[0] != "name" {
		t.Errorf("expected required ['name'], got %v", schema.Required)
	}
	if schema.ExtraFields == nil {
		t.Fatal("expected extra fields to be preserved")
	}
	if _, ok := schema.ExtraFields["additionalProperties"]; !ok {
		t.Error("expected 'additionalProperties' in extra fields")
	}
	if _, ok := schema.ExtraFields["$defs"]; !ok {
		t.Error("expected '$defs' in extra fields")
	}
}

func TestConvertRequest_Defaults(t *testing.T) {
	cfg := &Config{Model: "claude-sonnet-4-5-20250929", MaxTokens: 4096}
	req := provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hello"},
		},
	}

	params := convertRequest(req, cfg, nil)

	if params.MaxTokens != 4096 {
		t.Errorf("expected max_tokens 4096, got %d", params.MaxTokens)
	}
	if string(params.Model) != "claude-sonnet-4-5-20250929" {
		t.Errorf("expected model 'claude-sonnet-4-5-20250929', got %q", params.Model)
	}
}

func TestConvertRequest_MaxTokensOverride(t *testing.T) {
	cfg := &Config{Model: "claude-sonnet-4-5-20250929", MaxTokens: 4096}
	req := provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "Hello"},
		},
		MaxTokens: 8192,
	}

	params := convertRequest(req, cfg, nil)

	if params.MaxTokens != 8192 {
		t.Errorf("expected max_tokens override 8192, got %d", params.MaxTokens)
	}
}

// textBlock creates a ContentBlockUnion that behaves like a TextBlock.
func textBlock(text string) sdkanthropic.ContentBlockUnion {
	raw := `{"type":"text","text":` + jsonString(text) + `}`
	var block sdkanthropic.ContentBlockUnion
	_ = json.Unmarshal([]byte(raw), &block)
	return block
}

// toolUseBlock creates a ContentBlockUnion that behaves like a ToolUseBlock.
func toolUseBlock(id, name, input string) sdkanthropic.ContentBlockUnion {
	raw := `{"type":"tool_use","id":` + jsonString(id) + `,"name":` + jsonString(name) + `,"input":` + input + `}`
	var block sdkanthropic.ContentBlockUnion
	_ = json.Unmarshal([]byte(raw), &block)
	return block
}

// jsonString returns a JSON-encoded string value.
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
