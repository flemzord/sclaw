package openairesponses

import (
	"github.com/flemzord/sclaw/internal/provider"
)

// buildClientEvent converts a provider.CompletionRequest into a clientEvent
// ready to be sent over the WebSocket as a "response.create" event.
// The Responses API uses a flat structure with all fields at the top level.
func buildClientEvent(cfg Config, req provider.CompletionRequest) clientEvent {
	event := clientEvent{
		Type:  "response.create",
		Model: cfg.Model,
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = cfg.MaxTokens
	}
	event.MaxOutputTokens = maxTokens
	event.Temperature = req.Temperature

	// Convert messages to input items.
	// System messages are extracted into Instructions.
	for _, m := range req.Messages {
		switch m.Role {
		case provider.MessageRoleSystem:
			// Concatenate system messages into Instructions.
			if event.Instructions != "" {
				event.Instructions += "\n"
			}
			event.Instructions += m.Content

		case provider.MessageRoleUser:
			content := buildUserContent(m)
			if len(content) == 0 {
				continue
			}
			event.Input = append(event.Input, inputItem{
				Type:    "message",
				Role:    "user",
				Content: content,
			})

		case provider.MessageRoleAssistant:
			// If the assistant message has tool calls, emit one function_call per tool call.
			if len(m.ToolCalls) > 0 {
				// Emit text content first if present.
				if m.Content != "" {
					event.Input = append(event.Input, inputItem{
						Type:    "message",
						Role:    "assistant",
						Content: []inputContentPart{{Type: "output_text", Text: m.Content}},
					})
				}
				for _, tc := range m.ToolCalls {
					event.Input = append(event.Input, inputItem{
						Type:      "function_call",
						CallID:    tc.ID,
						Name:      tc.Name,
						Arguments: string(tc.Arguments),
					})
				}
			} else if m.Content != "" {
				event.Input = append(event.Input, inputItem{
					Type:    "message",
					Role:    "assistant",
					Content: []inputContentPart{{Type: "output_text", Text: m.Content}},
				})
			}

		case provider.MessageRoleTool:
			output := m.Content
			event.Input = append(event.Input, inputItem{
				Type:   "function_call_output",
				CallID: m.ToolID,
				Output: &output,
			})
		}
	}

	// Convert tool definitions.
	if len(req.Tools) > 0 {
		event.Tools = make([]wireTool, len(req.Tools))
		for i, t := range req.Tools {
			event.Tools[i] = wireTool{
				Type:        "function",
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			}
		}
	}

	return event
}

func buildUserContent(m provider.LLMMessage) []inputContentPart {
	if len(m.ContentParts) > 0 {
		return convertContentParts(m.ContentParts)
	}
	if m.Content == "" {
		return nil
	}
	return []inputContentPart{{Type: "input_text", Text: m.Content}}
}

// convertContentParts converts provider.ContentPart slices to wire format.
func convertContentParts(parts []provider.ContentPart) []inputContentPart {
	result := make([]inputContentPart, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case provider.ContentPartText:
			if p.Text != "" {
				result = append(result, inputContentPart{Type: "input_text", Text: p.Text})
			}
		case provider.ContentPartImageURL:
			if p.ImageURL == nil || p.ImageURL.URL == "" {
				continue
			}
			cp := inputContentPart{
				Type:     "input_image",
				ImageURL: p.ImageURL.URL,
			}
			if p.ImageURL.Detail != "" {
				cp.Detail = p.ImageURL.Detail
			}
			result = append(result, cp)
		}
	}
	return result
}

// mapStopReason converts a Responses API stop_reason to a provider.FinishReason.
func mapStopReason(reason string, hasToolCalls bool) provider.FinishReason {
	switch reason {
	case "stop", "end_turn", "completed":
		if hasToolCalls {
			return provider.FinishReasonToolUse
		}
		return provider.FinishReasonStop
	case "max_output_tokens", "length":
		return provider.FinishReasonLength
	case "content_filter":
		return provider.FinishReasonFiltering
	default:
		if hasToolCalls {
			return provider.FinishReasonToolUse
		}
		return provider.FinishReason(reason)
	}
}
