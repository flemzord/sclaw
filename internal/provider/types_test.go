package provider

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestLLMMessageJSONRoundTrip(t *testing.T) {
	t.Parallel()

	msg := LLMMessage{
		Role:    MessageRoleAssistant,
		Content: "hello",
		Name:    "alice",
		ToolCalls: []ToolCall{
			{ID: "tc1", Name: "search", Arguments: json.RawMessage(`{"query":"hello"}`)},
		},
		IsError: true,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got LLMMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual(got, msg) {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, msg)
	}
}

func TestLLMMessageOmitempty(t *testing.T) {
	t.Parallel()

	msg := LLMMessage{Role: MessageRoleSystem, Content: "you are helpful"}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	if _, ok := raw["name"]; ok {
		t.Error("expected name to be omitted when empty")
	}
	if _, ok := raw["tool_id"]; ok {
		t.Error("expected tool_id to be omitted when empty")
	}
	if _, ok := raw["tool_calls"]; ok {
		t.Error("expected tool_calls to be omitted when empty")
	}
	if _, ok := raw["is_error"]; ok {
		t.Error("expected is_error to be omitted when false")
	}
}

func TestToolCallArgumentsRawMessage(t *testing.T) {
	t.Parallel()

	tc := ToolCall{
		ID:        "call_1",
		Name:      "search",
		Arguments: json.RawMessage(`{"query":"test"}`),
	}

	data, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got ToolCall
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.ID != tc.ID || got.Name != tc.Name {
		t.Errorf("field mismatch: got %+v, want %+v", got, tc)
	}

	var args map[string]string
	if err := json.Unmarshal(got.Arguments, &args); err != nil {
		t.Fatalf("unmarshal arguments: %v", err)
	}
	if args["query"] != "test" {
		t.Errorf("arguments mismatch: got %v", args)
	}
}

func TestCompletionRequestOmitempty(t *testing.T) {
	t.Parallel()

	req := CompletionRequest{
		Messages: []LLMMessage{{Role: MessageRoleUser, Content: "hi"}},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	for _, key := range []string{"tools", "max_tokens", "temperature", "top_p", "stop"} {
		if _, ok := raw[key]; ok {
			t.Errorf("expected %s to be omitted when zero/nil", key)
		}
	}
}

func TestCompletionRequestWithTemperature(t *testing.T) {
	t.Parallel()

	temp := 0.7
	req := CompletionRequest{
		Messages:    []LLMMessage{{Role: MessageRoleUser, Content: "hi"}},
		Temperature: &temp,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got CompletionRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Temperature == nil {
		t.Fatal("expected temperature to be non-nil")
	}
	if *got.Temperature != temp {
		t.Errorf("temperature = %v, want %v", *got.Temperature, temp)
	}
}

func TestStreamChunkErrNotSerialized(t *testing.T) {
	t.Parallel()

	chunk := StreamChunk{
		Content: "hello",
		Err:     ErrProviderDown,
	}

	data, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	if _, ok := raw["err"]; ok {
		t.Error("expected Err field to be excluded from JSON")
	}
}

func TestCompletionResponseRoundTrip(t *testing.T) {
	t.Parallel()

	resp := CompletionResponse{
		Content:      "The answer is 42.",
		FinishReason: FinishReasonStop,
		Usage: TokenUsage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
		ToolCalls: []ToolCall{
			{ID: "tc1", Name: "calc", Arguments: json.RawMessage(`{"expr":"6*7"}`)},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got CompletionResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Content != resp.Content {
		t.Errorf("content = %q, want %q", got.Content, resp.Content)
	}
	if got.FinishReason != resp.FinishReason {
		t.Errorf("finish_reason = %q, want %q", got.FinishReason, resp.FinishReason)
	}
	if got.Usage != resp.Usage {
		t.Errorf("usage = %+v, want %+v", got.Usage, resp.Usage)
	}
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].Name != "calc" {
		t.Errorf("tool_calls mismatch: got %+v", got.ToolCalls)
	}
}

func TestTextForDisplay_ContentOnly(t *testing.T) {
	t.Parallel()
	msg := LLMMessage{Content: "hello world"}
	if got := msg.TextForDisplay(); got != "hello world" {
		t.Errorf("TextForDisplay() = %q, want %q", got, "hello world")
	}
}

func TestTextForDisplay_ContentParts(t *testing.T) {
	t.Parallel()
	msg := LLMMessage{
		ContentParts: []ContentPart{
			{Type: ContentPartText, Text: "Look at this"},
			{Type: ContentPartImageURL, ImageURL: &ImageURL{URL: "https://example.com/img.jpg"}},
			{Type: ContentPartText, Text: "What do you see?"},
		},
	}
	want := "Look at this\nWhat do you see?"
	if got := msg.TextForDisplay(); got != want {
		t.Errorf("TextForDisplay() = %q, want %q", got, want)
	}
}

func TestTextForDisplay_ImageOnly(t *testing.T) {
	t.Parallel()
	msg := LLMMessage{
		ContentParts: []ContentPart{
			{Type: ContentPartImageURL, ImageURL: &ImageURL{URL: "https://example.com/img.jpg"}},
		},
	}
	if got := msg.TextForDisplay(); got != "" {
		t.Errorf("TextForDisplay() = %q, want empty", got)
	}
}

func TestTextForDisplay_ContentTakesPrecedence(t *testing.T) {
	t.Parallel()
	msg := LLMMessage{
		Content:      "plain text",
		ContentParts: []ContentPart{{Type: ContentPartText, Text: "part text"}},
	}
	if got := msg.TextForDisplay(); got != "plain text" {
		t.Errorf("TextForDisplay() = %q, want %q (Content takes precedence)", got, "plain text")
	}
}

func TestHasImages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  LLMMessage
		want bool
	}{
		{"no parts", LLMMessage{Content: "hi"}, false},
		{"text parts only", LLMMessage{ContentParts: []ContentPart{{Type: ContentPartText, Text: "hi"}}}, false},
		{"with image", LLMMessage{ContentParts: []ContentPart{
			{Type: ContentPartText, Text: "look"},
			{Type: ContentPartImageURL, ImageURL: &ImageURL{URL: "https://example.com/img.jpg"}},
		}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.msg.HasImages(); got != tt.want {
				t.Errorf("HasImages() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLLMMessageContentPartsOmitempty(t *testing.T) {
	t.Parallel()
	msg := LLMMessage{Role: MessageRoleUser, Content: "hi"}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := raw["content_parts"]; ok {
		t.Error("expected content_parts to be omitted when nil")
	}
}

func TestContentPartJSONRoundTrip(t *testing.T) {
	t.Parallel()
	msg := LLMMessage{
		Role: MessageRoleUser,
		ContentParts: []ContentPart{
			{Type: ContentPartText, Text: "describe this"},
			{Type: ContentPartImageURL, ImageURL: &ImageURL{URL: "https://img.com/a.png", Detail: "auto"}},
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got LLMMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.ContentParts) != 2 {
		t.Fatalf("content_parts len = %d, want 2", len(got.ContentParts))
	}
	if got.ContentParts[0].Type != ContentPartText || got.ContentParts[0].Text != "describe this" {
		t.Errorf("part[0] = %+v", got.ContentParts[0])
	}
	if got.ContentParts[1].Type != ContentPartImageURL || got.ContentParts[1].ImageURL.URL != "https://img.com/a.png" {
		t.Errorf("part[1] = %+v", got.ContentParts[1])
	}
}

func TestRoleConstants(t *testing.T) {
	t.Parallel()

	roles := map[Role]string{
		RolePrimary:  "primary",
		RoleInternal: "internal",
		RoleFallback: "fallback",
	}
	for r, want := range roles {
		if string(r) != want {
			t.Errorf("Role %v = %q, want %q", r, string(r), want)
		}
	}
}

func TestFinishReasonConstants(t *testing.T) {
	t.Parallel()

	reasons := map[FinishReason]string{
		FinishReasonStop:      "stop",
		FinishReasonLength:    "length",
		FinishReasonToolUse:   "tool_use",
		FinishReasonFiltering: "filtering",
	}
	for r, want := range reasons {
		if string(r) != want {
			t.Errorf("FinishReason %v = %q, want %q", r, string(r), want)
		}
	}
}
