package openrouter

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/flemzord/sclaw/internal/provider"
)

func TestBuildRequest(t *testing.T) {
	t.Parallel()

	temp := 0.7
	topP := 0.9
	o := &OpenRouter{
		config: Config{
			Model:   "openai/gpt-4o",
			BaseURL: defaultBaseURL,
		},
	}

	req := provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleSystem, Content: "You are helpful."},
			{Role: provider.MessageRoleUser, Content: "Hello"},
		},
		Tools: []provider.ToolDefinition{
			{
				Name:        "get_weather",
				Description: "Get weather",
				Parameters:  json.RawMessage(`{"type":"object"}`),
			},
		},
		MaxTokens:   100,
		Temperature: &temp,
		TopP:        &topP,
		Stop:        []string{"\n\n"},
	}

	apiReq := o.buildRequest(req, false)

	if apiReq.Model != "openai/gpt-4o" {
		t.Errorf("Model = %q", apiReq.Model)
	}
	if apiReq.Stream {
		t.Error("Stream should be false")
	}
	if len(apiReq.Messages) != 2 {
		t.Fatalf("Messages count = %d, want 2", len(apiReq.Messages))
	}
	if apiReq.Messages[0].Role != "system" {
		t.Errorf("Messages[0].Role = %q", apiReq.Messages[0].Role)
	}
	if apiReq.MaxTokens != 100 {
		t.Errorf("MaxTokens = %d", apiReq.MaxTokens)
	}
	if *apiReq.Temperature != 0.7 {
		t.Errorf("Temperature = %f", *apiReq.Temperature)
	}
	if len(apiReq.Tools) != 1 {
		t.Fatalf("Tools count = %d, want 1", len(apiReq.Tools))
	}
	if apiReq.Tools[0].Function.Name != "get_weather" {
		t.Errorf("Tools[0].Function.Name = %q", apiReq.Tools[0].Function.Name)
	}
}

func TestBuildRequestAutoModel(t *testing.T) {
	t.Parallel()

	o := &OpenRouter{
		config: Config{Model: "auto"},
	}

	apiReq := o.buildRequest(provider.CompletionRequest{}, false)
	if apiReq.Model != "openrouter/auto" {
		t.Errorf("Model = %q, want %q", apiReq.Model, "openrouter/auto")
	}
}

func TestBuildRequestStream(t *testing.T) {
	t.Parallel()

	o := &OpenRouter{config: Config{Model: "test"}}
	apiReq := o.buildRequest(provider.CompletionRequest{}, true)
	if !apiReq.Stream {
		t.Error("Stream should be true")
	}
}

func TestConvertMessages(t *testing.T) {
	t.Parallel()

	msgs := []provider.LLMMessage{
		{
			Role:    provider.MessageRoleAssistant,
			Content: "I'll help.",
			ToolCalls: []provider.ToolCall{
				{
					ID:        "call_1",
					Name:      "search",
					Arguments: json.RawMessage(`{"q":"go"}`),
				},
			},
		},
		{
			Role:    provider.MessageRoleTool,
			Content: `{"results":[]}`,
			ToolID:  "call_1",
		},
	}

	result := convertMessages(msgs)

	if len(result) != 2 {
		t.Fatalf("got %d messages, want 2", len(result))
	}

	// Assistant with tool calls.
	am := result[0]
	if am.Role != "assistant" {
		t.Errorf("Role = %q", am.Role)
	}
	if len(am.ToolCalls) != 1 {
		t.Fatalf("ToolCalls count = %d", len(am.ToolCalls))
	}
	if am.ToolCalls[0].Type != "function" {
		t.Errorf("ToolCalls[0].Type = %q", am.ToolCalls[0].Type)
	}
	if am.ToolCalls[0].Function.Name != "search" {
		t.Errorf("ToolCalls[0].Function.Name = %q", am.ToolCalls[0].Function.Name)
	}

	// Tool result.
	tm := result[1]
	if tm.Role != "tool" {
		t.Errorf("Role = %q", tm.Role)
	}
	if tm.ToolCallID != "call_1" {
		t.Errorf("ToolCallID = %q", tm.ToolCallID)
	}
}

func TestConvertResponse(t *testing.T) {
	t.Parallel()

	resp := apiResponse{
		Choices: []apiChoice{
			{
				Message: apiMessage{
					Role:    "assistant",
					Content: "Hello!",
					ToolCalls: []apiToolCall{
						{
							ID:   "call_42",
							Type: "function",
							Function: apiToolCallFn{
								Name:      "greet",
								Arguments: `{"name":"world"}`,
							},
						},
					},
				},
				FinishReason: "tool_calls",
			},
		},
		Usage: apiUsage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}

	cr := convertResponse(resp)

	if cr.Content != "Hello!" {
		t.Errorf("Content = %q", cr.Content)
	}
	if cr.FinishReason != provider.FinishReasonToolUse {
		t.Errorf("FinishReason = %q", cr.FinishReason)
	}
	if cr.Usage.TotalTokens != 30 {
		t.Errorf("Usage.TotalTokens = %d", cr.Usage.TotalTokens)
	}
	if len(cr.ToolCalls) != 1 {
		t.Fatalf("ToolCalls count = %d", len(cr.ToolCalls))
	}
	if cr.ToolCalls[0].Name != "greet" {
		t.Errorf("ToolCalls[0].Name = %q", cr.ToolCalls[0].Name)
	}
}

func TestConvertResponseEmpty(t *testing.T) {
	t.Parallel()

	cr := convertResponse(apiResponse{})
	if cr.Content != "" {
		t.Errorf("Content = %q, want empty", cr.Content)
	}
}

func TestDoRequestHeaders(t *testing.T) {
	t.Parallel()

	var capturedHeaders http.Header
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[],"usage":{}}`))
	})

	o := &OpenRouter{
		config: Config{
			APIKey:  "sk-test-key",
			Model:   "test/model",
			BaseURL: srv.URL,
			Referer: "https://myapp.com",
			Title:   "TestApp",
		},
		client: srv.Client(),
	}

	_, err := o.Complete(t.Context(), provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "hi"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	if got := capturedHeaders.Get("Authorization"); got != "Bearer sk-test-key" {
		t.Errorf("Authorization = %q", got)
	}
	if got := capturedHeaders.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := capturedHeaders.Get("HTTP-Referer"); got != "https://myapp.com" {
		t.Errorf("HTTP-Referer = %q", got)
	}
	if got := capturedHeaders.Get("X-Title"); got != "TestApp" {
		t.Errorf("X-Title = %q", got)
	}
}

func TestContextWindowSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		model         string
		contextWindow int
		want          int
	}{
		{name: "known model", model: "openai/gpt-4o", want: 128000},
		{name: "unknown model", model: "unknown/model", want: defaultContextWindow},
		{name: "config override", model: "openai/gpt-4o", contextWindow: 4096, want: 4096},
		{name: "auto model", model: "auto", want: 128000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			o := &OpenRouter{config: Config{Model: tt.model, ContextWindow: tt.contextWindow}}
			if got := o.ContextWindowSize(); got != tt.want {
				t.Errorf("ContextWindowSize() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestModelName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		model string
		want  string
	}{
		{"openai/gpt-4o", "openai/gpt-4o"},
		{"auto", "openrouter/auto"},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			t.Parallel()
			o := &OpenRouter{config: Config{Model: tt.model}}
			if got := o.ModelName(); got != tt.want {
				t.Errorf("ModelName() = %q, want %q", got, tt.want)
			}
		})
	}
}
