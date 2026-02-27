package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/flemzord/sclaw/internal/provider"
)

// openAI wire types for JSON serialization.

type oaiRequest struct {
	Model         string               `json:"model"`
	Messages      []oaiRequestMessage  `json:"messages"`
	Tools         []oaiTool            `json:"tools,omitempty"`
	Stream        bool                 `json:"stream,omitempty"`
	StreamOptions *oaiStreamOptions    `json:"stream_options,omitempty"`
	MaxTokens     int                  `json:"max_tokens,omitempty"`
	Temperature   *float64             `json:"temperature,omitempty"`
	TopP          *float64             `json:"top_p,omitempty"`
	Stop          []string             `json:"stop,omitempty"`
}

// oaiStreamOptions controls streaming behavior.
type oaiStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// oaiRequestMessage is the wire type for outgoing messages in API requests.
// Content is any so that json.Marshal produces a string for text-only messages
// and an array of content parts for multimodal messages.
type oaiRequestMessage struct {
	Role       string        `json:"role"`
	Content    any           `json:"content"`
	Name       string        `json:"name,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
}

// oaiContentPart is a single element in a multimodal content array.
type oaiContentPart struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	ImageURL *oaiImageURL `json:"image_url,omitempty"`
}

// oaiImageURL holds the URL and detail level for an image content part.
type oaiImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// oaiMessage is the wire type for incoming messages in API responses.
// Content is always a string because LLMs do not return multimodal responses.
type oaiMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content"`
	Name       string        `json:"name,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
}

type oaiToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function oaiToolFunction `json:"function"`
}

type oaiToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type oaiTool struct {
	Type     string     `json:"type"`
	Function oaiToolDef `json:"function"`
}

type oaiToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type oaiResponse struct {
	Choices []oaiChoice `json:"choices"`
	Usage   oaiUsage    `json:"usage"`
}

type oaiChoice struct {
	Message      oaiMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
}

type oaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// buildRequest converts a provider.CompletionRequest into an oaiRequest.
// configMaxTokens is used as a fallback when req.MaxTokens is zero.
func buildRequest(model string, configMaxTokens int, req provider.CompletionRequest, stream bool) oaiRequest {
	messages := make([]oaiRequestMessage, len(req.Messages))
	for i, m := range req.Messages {
		msg := oaiRequestMessage{
			Role: string(m.Role),
			Name: m.Name,
		}

		// Multimodal: serialize content as array of parts.
		if len(m.ContentParts) > 0 {
			parts := make([]oaiContentPart, 0, len(m.ContentParts))
			for _, p := range m.ContentParts {
				switch p.Type {
				case provider.ContentPartText:
					parts = append(parts, oaiContentPart{Type: "text", Text: p.Text})
				case provider.ContentPartImageURL:
					cp := oaiContentPart{
						Type:     "image_url",
						ImageURL: &oaiImageURL{URL: p.ImageURL.URL, Detail: p.ImageURL.Detail},
					}
					parts = append(parts, cp)
				}
			}
			msg.Content = parts
		} else {
			msg.Content = m.Content
		}

		if m.ToolID != "" {
			msg.ToolCallID = m.ToolID
		}
		if len(m.ToolCalls) > 0 {
			msg.ToolCalls = make([]oaiToolCall, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				msg.ToolCalls[j] = oaiToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: oaiToolFunction{
						Name:      tc.Name,
						Arguments: string(tc.Arguments),
					},
				}
			}
		}
		messages[i] = msg
	}

	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = configMaxTokens
	}

	oai := oaiRequest{
		Model:       model,
		Messages:    messages,
		Stream:      stream,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stop:        req.Stop,
	}

	// Request usage stats in the final streaming chunk so callers
	// can track token consumption even in streaming mode.
	if stream {
		oai.StreamOptions = &oaiStreamOptions{IncludeUsage: true}
	}

	if len(req.Tools) > 0 {
		oai.Tools = make([]oaiTool, len(req.Tools))
		for i, t := range req.Tools {
			oai.Tools[i] = oaiTool{
				Type: "function",
				Function: oaiToolDef{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.Parameters,
				},
			}
		}
	}

	return oai
}

// parseResponse converts an oaiResponse into a provider.CompletionResponse.
func parseResponse(resp oaiResponse) provider.CompletionResponse {
	var cr provider.CompletionResponse
	cr.Usage = provider.TokenUsage{
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
	}

	if len(resp.Choices) == 0 {
		return cr
	}

	choice := resp.Choices[0]
	cr.Content = choice.Message.Content
	cr.FinishReason = mapFinishReason(choice.FinishReason)

	if len(choice.Message.ToolCalls) > 0 {
		cr.ToolCalls = make([]provider.ToolCall, len(choice.Message.ToolCalls))
		for i, tc := range choice.Message.ToolCalls {
			cr.ToolCalls[i] = provider.ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: json.RawMessage(tc.Function.Arguments),
			}
		}
	}

	return cr
}

// mapFinishReason converts an OpenAI finish_reason string to a provider.FinishReason.
func mapFinishReason(reason string) provider.FinishReason {
	switch reason {
	case "stop":
		return provider.FinishReasonStop
	case "length":
		return provider.FinishReasonLength
	case "tool_calls":
		return provider.FinishReasonToolUse
	case "content_filter":
		return provider.FinishReasonFiltering
	default:
		// Pass through unknown finish reasons rather than silently
		// converting them to "stop", which could mask provider-specific values.
		return provider.FinishReason(reason)
	}
}

// doRequest executes an HTTP POST to the chat completions endpoint.
func (p *Provider) doRequest(ctx context.Context, body oaiRequest) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	endpoint := p.config.BaseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	for k, v := range p.config.Headers {
		req.Header.Set(k, v)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		// Do not classify caller cancellation/timeout as provider failure;
		// that would incorrectly degrade health in the chain.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %w", provider.ErrProviderDown, err)
	}

	return resp, nil
}

// maxErrorBodySize caps how much of an error response body is read to prevent memory spikes.
const maxErrorBodySize = 4096

// handleErrorResponse maps HTTP error status codes to sentinel errors.
func handleErrorResponse(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("%w: %s", provider.ErrRateLimit, body)
	case resp.StatusCode >= 500:
		return fmt.Errorf("%w: HTTP %d: %s", provider.ErrProviderDown, resp.StatusCode, body)
	case resp.StatusCode == http.StatusBadRequest:
		if isContextLengthError(body) {
			return fmt.Errorf("%w: %s", provider.ErrContextLength, body)
		}
		return fmt.Errorf("bad request: %s", body)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w: HTTP %d: %s", provider.ErrAuthentication, resp.StatusCode, body)
	default:
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, body)
	}
}

// isContextLengthError checks if an error body indicates a context length exceeded error.
func isContextLengthError(body []byte) bool {
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "context_length_exceeded") ||
		strings.Contains(lower, "context length") ||
		strings.Contains(lower, "maximum context") ||
		strings.Contains(lower, "token limit")
}
