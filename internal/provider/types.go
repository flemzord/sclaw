package provider

import (
	"encoding/json"
	"strings"
)

// Role describes the purpose a provider serves in the system.
type Role string

// Role constants for provider chain configuration.
const (
	RolePrimary  Role = "primary"
	RoleInternal Role = "internal"
	RoleFallback Role = "fallback"
)

// MessageRole identifies the sender of a message in a conversation.
type MessageRole string

// MessageRole constants for conversation messages.
const (
	MessageRoleSystem    MessageRole = "system"
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleTool      MessageRole = "tool"
)

// FinishReason describes why the model stopped generating.
type FinishReason string

// FinishReason constants for model completion termination.
const (
	FinishReasonStop      FinishReason = "stop"
	FinishReasonLength    FinishReason = "length"
	FinishReasonToolUse   FinishReason = "tool_use"
	FinishReasonFiltering FinishReason = "filtering"
)

// ContentPartType discriminates the variant of a ContentPart.
type ContentPartType string

const (
	ContentPartText     ContentPartType = "text"
	ContentPartImageURL ContentPartType = "image_url"
)

// ContentPart represents one element of a multimodal message.
type ContentPart struct {
	Type     ContentPartType `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *ImageURL       `json:"image_url,omitempty"`
}

// ImageURL holds the URL and optional detail level for an image part.
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "auto", "low", "high"
}

// LLMMessage represents a single message in a conversation.
// When ContentParts is non-nil it is the source of truth for message content;
// Content remains empty. Text-only messages use Content alone (ContentParts nil).
type LLMMessage struct {
	Role         MessageRole   `json:"role"`
	Content      string        `json:"content"`
	ContentParts []ContentPart `json:"content_parts,omitempty"`
	Name         string        `json:"name,omitempty"`
	ToolID       string        `json:"tool_id,omitempty"`
	ToolCalls    []ToolCall    `json:"tool_calls,omitempty"`
	IsError      bool          `json:"is_error,omitempty"`
}

// TextForDisplay returns the text representation of the message content.
// If Content is set it is returned directly; otherwise text parts from
// ContentParts are concatenated. Useful for consumers that only need text
// (memory extraction, logging, persistence).
func (m LLMMessage) TextForDisplay() string {
	if m.Content != "" {
		return m.Content
	}
	var b strings.Builder
	for _, p := range m.ContentParts {
		if p.Type == ContentPartText && p.Text != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// HasImages reports whether the message contains image content parts.
func (m LLMMessage) HasImages() bool {
	for _, p := range m.ContentParts {
		if p.Type == ContentPartImageURL {
			return true
		}
	}
	return false
}

// ToolCall represents a tool invocation requested by the model.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolDefinition describes a tool the model may invoke.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// CompletionRequest is the input to a Provider.Complete or Provider.Stream call.
type CompletionRequest struct {
	Messages    []LLMMessage     `json:"messages"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature *float64         `json:"temperature,omitempty"`
	TopP        *float64         `json:"top_p,omitempty"`
	Stop        []string         `json:"stop,omitempty"`
}

// CompletionResponse is the output of a Provider.Complete call.
type CompletionResponse struct {
	Content      string       `json:"content"`
	ToolCalls    []ToolCall   `json:"tool_calls,omitempty"`
	FinishReason FinishReason `json:"finish_reason"`
	Usage        TokenUsage   `json:"usage"`
}

// StreamChunk represents one piece of a streaming completion response.
type StreamChunk struct {
	Content      string       `json:"content,omitempty"`
	ToolCalls    []ToolCall   `json:"tool_calls,omitempty"`
	FinishReason FinishReason `json:"finish_reason,omitempty"`
	Usage        *TokenUsage  `json:"usage,omitempty"`
	Err          error        `json:"-"`
}

// TokenUsage tracks token consumption for a completion.
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
