package anthropic

import (
	"encoding/json"
	"log/slog"

	sdkanthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/flemzord/sclaw/internal/provider"
)

// convertRequest transforms a sclaw CompletionRequest into Anthropic SDK parameters.
// System messages are extracted from the message list into the dedicated System field.
func convertRequest(req provider.CompletionRequest, cfg *Config, logger *slog.Logger) sdkanthropic.MessageNewParams {
	system, messages := splitSystemMessages(req.Messages)

	params := sdkanthropic.MessageNewParams{
		Model:    sdkanthropic.Model(cfg.Model),
		Messages: convertMessages(messages, logger),
		System:   system,
	}

	// MaxTokens: request-level override takes precedence over config default.
	params.MaxTokens = int64(cfg.MaxTokens)
	if req.MaxTokens > 0 {
		params.MaxTokens = int64(req.MaxTokens)
	}

	if req.Temperature != nil {
		params.Temperature = sdkanthropic.Float(*req.Temperature)
	}
	if req.TopP != nil {
		params.TopP = sdkanthropic.Float(*req.TopP)
	}
	if len(req.Stop) > 0 {
		params.StopSequences = req.Stop
	}
	if len(req.Tools) > 0 {
		params.Tools = convertTools(req.Tools)
	}

	return params
}

// splitSystemMessages extracts leading system messages into Anthropic's System
// parameter format and returns the remaining messages.
func splitSystemMessages(msgs []provider.LLMMessage) ([]sdkanthropic.TextBlockParam, []provider.LLMMessage) {
	var system []sdkanthropic.TextBlockParam
	var idx int
	for idx = 0; idx < len(msgs); idx++ {
		if msgs[idx].Role != provider.MessageRoleSystem {
			break
		}
		system = append(system, sdkanthropic.TextBlockParam{
			Text: msgs[idx].Content,
		})
	}
	return system, msgs[idx:]
}

// convertMessages transforms sclaw messages into Anthropic SDK message params.
// Consecutive tool-result messages are grouped into a single user message
// (Anthropic requires all tool results for a turn in one message).
// Non-leading system messages are logged as warnings since the Anthropic API
// only supports system messages as a separate parameter, not inline.
func convertMessages(msgs []provider.LLMMessage, logger *slog.Logger) []sdkanthropic.MessageParam {
	var result []sdkanthropic.MessageParam

	for i := 0; i < len(msgs); {
		msg := msgs[i]

		switch msg.Role {
		case provider.MessageRoleTool:
			// Collect consecutive tool result messages into one user message.
			var blocks []sdkanthropic.ContentBlockParamUnion
			for i < len(msgs) && msgs[i].Role == provider.MessageRoleTool {
				blocks = append(blocks, sdkanthropic.NewToolResultBlock(
					msgs[i].ToolID,
					msgs[i].Content,
					msgs[i].IsError,
				))
				i++
			}
			result = append(result, sdkanthropic.MessageParam{
				Role:    sdkanthropic.MessageParamRoleUser,
				Content: blocks,
			})

		case provider.MessageRoleAssistant:
			result = append(result, convertAssistantMessage(msg))
			i++

		case provider.MessageRoleUser:
			result = append(result, sdkanthropic.NewUserMessage(
				sdkanthropic.NewTextBlock(msg.Content),
			))
			i++

		case provider.MessageRoleSystem:
			// Non-leading system messages cannot be sent to the Anthropic API.
			// They are already extracted by splitSystemMessages; any remaining
			// ones indicate a mid-conversation system message which is dropped.
			if logger != nil {
				logger.Warn("dropping non-leading system message; Anthropic API only supports system messages at the start",
					"index", i,
				)
			}
			i++

		default:
			i++
		}
	}

	return result
}

// convertAssistantMessage converts an assistant message, including any tool
// calls, into an Anthropic assistant message with mixed content blocks.
func convertAssistantMessage(msg provider.LLMMessage) sdkanthropic.MessageParam {
	var blocks []sdkanthropic.ContentBlockParamUnion

	if msg.Content != "" {
		blocks = append(blocks, sdkanthropic.NewTextBlock(msg.Content))
	}

	for _, tc := range msg.ToolCalls {
		// Pass raw JSON directly — json.RawMessage implements json.Marshaler
		// so the SDK will serialize it correctly without double-encoding.
		input := any(tc.Arguments)
		if len(tc.Arguments) == 0 {
			input = json.RawMessage("{}")
		}
		blocks = append(blocks, sdkanthropic.NewToolUseBlock(tc.ID, input, tc.Name))
	}

	return sdkanthropic.NewAssistantMessage(blocks...)
}

// convertTools transforms sclaw tool definitions into Anthropic SDK tool params.
func convertTools(tools []provider.ToolDefinition) []sdkanthropic.ToolUnionParam {
	result := make([]sdkanthropic.ToolUnionParam, len(tools))
	for i, t := range tools {
		tool := &sdkanthropic.ToolParam{
			Name: t.Name,
		}
		if t.Description != "" {
			tool.Description = sdkanthropic.String(t.Description)
		}
		if len(t.Parameters) > 0 {
			tool.InputSchema = convertInputSchema(t.Parameters)
		}
		result[i] = sdkanthropic.ToolUnionParam{OfTool: tool}
	}
	return result
}

// convertInputSchema converts a raw JSON Schema into the SDK's ToolInputSchemaParam.
// All schema fields beyond "properties" and "required" (e.g. $defs, oneOf,
// additionalProperties, enum) are preserved via ExtraFields.
func convertInputSchema(raw json.RawMessage) sdkanthropic.ToolInputSchemaParam {
	var full map[string]any
	if err := json.Unmarshal(raw, &full); err != nil {
		return sdkanthropic.ToolInputSchemaParam{}
	}

	param := sdkanthropic.ToolInputSchemaParam{}

	if props, ok := full["properties"]; ok {
		param.Properties = props
		delete(full, "properties")
	}
	if req, ok := full["required"]; ok {
		if arr, ok := req.([]any); ok {
			strs := make([]string, 0, len(arr))
			for _, v := range arr {
				if s, ok := v.(string); ok {
					strs = append(strs, s)
				}
			}
			param.Required = strs
		}
		delete(full, "required")
	}
	// "type" is auto-set to "object" by the SDK.
	delete(full, "type")

	if len(full) > 0 {
		param.ExtraFields = full
	}

	return param
}

// convertResponse transforms an Anthropic SDK Message into a sclaw CompletionResponse.
func convertResponse(msg *sdkanthropic.Message) provider.CompletionResponse {
	var content string
	var toolCalls []provider.ToolCall

	for _, block := range msg.Content {
		switch v := block.AsAny().(type) {
		case sdkanthropic.TextBlock:
			if content != "" {
				content += "\n"
			}
			content += v.Text
		case sdkanthropic.ToolUseBlock:
			toolCalls = append(toolCalls, provider.ToolCall{
				ID:        v.ID,
				Name:      v.Name,
				Arguments: v.Input,
			})
		}
	}

	return provider.CompletionResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		FinishReason: convertStopReason(msg.StopReason),
		Usage: provider.TokenUsage{
			PromptTokens:     int(msg.Usage.InputTokens),
			CompletionTokens: int(msg.Usage.OutputTokens),
			TotalTokens:      int(msg.Usage.InputTokens + msg.Usage.OutputTokens),
		},
	}
}

// convertStopReason maps an Anthropic stop reason to a sclaw FinishReason.
func convertStopReason(reason sdkanthropic.StopReason) provider.FinishReason {
	switch reason {
	case sdkanthropic.StopReasonEndTurn, sdkanthropic.StopReasonStopSequence:
		return provider.FinishReasonStop
	case sdkanthropic.StopReasonMaxTokens:
		return provider.FinishReasonLength
	case sdkanthropic.StopReasonToolUse:
		return provider.FinishReasonToolUse
	case sdkanthropic.StopReasonRefusal:
		return provider.FinishReasonFiltering
	default:
		return provider.FinishReasonStop
	}
}
