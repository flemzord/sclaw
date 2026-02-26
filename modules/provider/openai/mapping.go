package openai

import (
	"encoding/json"

	"github.com/flemzord/sclaw/internal/provider"
)

// --- OpenAI API request/response types (unexported, serialization only) ---

type chatRequest struct {
	Model         string        `json:"model"`
	Messages      []chatMessage `json:"messages"`
	Tools         []chatTool    `json:"tools,omitempty"`
	MaxTokens     int           `json:"max_tokens,omitempty"`
	Temperature   *float64      `json:"temperature,omitempty"`
	TopP          *float64      `json:"top_p,omitempty"`
	Stop          []string      `json:"stop,omitempty"`
	Stream        bool          `json:"stream,omitempty"`
	StreamOptions *streamOpts   `json:"stream_options,omitempty"`
}

type streamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	Name       string         `json:"name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type chatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function chatFunctionCall `json:"function"`
}

type chatFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Usage   chatUsage    `json:"usage"`
}

type chatChoice struct {
	Message      chatMessage `json:"message"`
	FinishReason *string     `json:"finish_reason"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type apiError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// --- Streaming types ---

type chatStreamChunk struct {
	Choices []chatStreamChoice `json:"choices"`
	Usage   *chatUsage         `json:"usage,omitempty"`
}

type chatStreamChoice struct {
	Delta        chatStreamDelta `json:"delta"`
	FinishReason *string         `json:"finish_reason"`
}

type chatStreamDelta struct {
	Role      string              `json:"role,omitempty"`
	Content   string              `json:"content,omitempty"`
	ToolCalls []chatToolCallDelta `json:"tool_calls,omitempty"`
}

type chatToolCallDelta struct {
	Index    int              `json:"index"`
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function chatFunctionCall `json:"function,omitempty"`
}

// --- Converter functions ---

// toMessages converts provider messages to OpenAI API messages.
func toMessages(msgs []provider.LLMMessage) []chatMessage {
	out := make([]chatMessage, len(msgs))
	for i, m := range msgs {
		cm := chatMessage{
			Role:    string(m.Role),
			Content: m.Content,
			Name:    m.Name,
		}
		if m.ToolID != "" {
			cm.ToolCallID = m.ToolID
		}
		if len(m.ToolCalls) > 0 {
			cm.ToolCalls = make([]chatToolCall, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				cm.ToolCalls[j] = chatToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: chatFunctionCall{
						Name:      tc.Name,
						Arguments: string(tc.Arguments),
					},
				}
			}
		}
		out[i] = cm
	}
	return out
}

// toTools converts provider tool definitions to OpenAI API tool format.
func toTools(tools []provider.ToolDefinition) []chatTool {
	out := make([]chatTool, len(tools))
	for i, t := range tools {
		out[i] = chatTool{
			Type: "function",
			Function: chatFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		}
	}
	return out
}

// fromResponse converts an OpenAI API response to a provider CompletionResponse.
func fromResponse(resp *chatResponse) provider.CompletionResponse {
	var cr provider.CompletionResponse
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		cr.Content = choice.Message.Content
		cr.FinishReason = mapFinishReason(choice.FinishReason)
		cr.ToolCalls = fromToolCalls(choice.Message.ToolCalls)
	}
	cr.Usage = provider.TokenUsage{
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
	}
	return cr
}

// fromToolCalls converts OpenAI API tool calls to provider ToolCalls.
func fromToolCalls(calls []chatToolCall) []provider.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]provider.ToolCall, len(calls))
	for i, c := range calls {
		out[i] = provider.ToolCall{
			ID:        c.ID,
			Name:      c.Function.Name,
			Arguments: json.RawMessage(c.Function.Arguments),
		}
	}
	return out
}

// mapFinishReason converts an OpenAI finish_reason string to a provider FinishReason.
func mapFinishReason(reason *string) provider.FinishReason {
	if reason == nil {
		return ""
	}
	switch *reason {
	case "stop":
		return provider.FinishReasonStop
	case "length":
		return provider.FinishReasonLength
	case "tool_calls":
		return provider.FinishReasonToolUse
	case "content_filter":
		return provider.FinishReasonFiltering
	default:
		return provider.FinishReason(*reason)
	}
}
