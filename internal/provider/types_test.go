package provider

import (
	"encoding/json"
	"testing"
)

func TestLLMMessageJSONRoundTrip(t *testing.T) {
	t.Parallel()

	msg := LLMMessage{
		Role:    MessageRoleUser,
		Content: "hello",
		Name:    "alice",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got LLMMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got != msg {
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
