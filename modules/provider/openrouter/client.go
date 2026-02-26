package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/flemzord/sclaw/internal/provider"
)

// apiRequest is the OpenAI-compatible chat completion request body.
type apiRequest struct {
	Model       string       `json:"model"`
	Messages    []apiMessage `json:"messages"`
	Tools       []apiTool    `json:"tools,omitempty"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	Temperature *float64     `json:"temperature,omitempty"`
	TopP        *float64     `json:"top_p,omitempty"`
	Stop        []string     `json:"stop,omitempty"`
	Stream      bool         `json:"stream"`
}

// apiMessage is an OpenAI-compatible chat message.
type apiMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content"`
	Name       string        `json:"name,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	ToolCalls  []apiToolCall `json:"tool_calls,omitempty"`
}

// apiTool describes a tool for the OpenAI-compatible API.
type apiTool struct {
	Type     string      `json:"type"`
	Function apiFunction `json:"function"`
}

// apiFunction is the function description inside an apiTool.
type apiFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// apiToolCall is an OpenAI-compatible tool call in a response.
type apiToolCall struct {
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Function apiToolCallFn `json:"function"`
}

// apiToolCallFn holds the function name and arguments in a tool call.
type apiToolCallFn struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// apiResponse is the non-streaming OpenAI-compatible response.
type apiResponse struct {
	Choices []apiChoice `json:"choices"`
	Usage   apiUsage    `json:"usage"`
	Error   struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// apiChoice is a single choice in a completion response.
type apiChoice struct {
	Message      apiMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
}

// apiStreamChunk is a single chunk in a streaming response.
type apiStreamChunk struct {
	Choices []apiStreamChoice `json:"choices"`
	Usage   *apiUsage         `json:"usage,omitempty"`
	Error   struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error,omitempty"`
}

// apiStreamChoice is a choice within a streaming chunk.
type apiStreamChoice struct {
	Delta        apiStreamDelta `json:"delta"`
	FinishReason string         `json:"finish_reason"`
}

// apiStreamDelta holds incremental content in a streaming chunk.
type apiStreamDelta struct {
	Content   string              `json:"content,omitempty"`
	ToolCalls []apiStreamToolCall `json:"tool_calls,omitempty"`
}

// apiStreamToolCall is a streaming tool call delta.
type apiStreamToolCall struct {
	Index    int           `json:"index"`
	ID       string        `json:"id,omitempty"`
	Type     string        `json:"type,omitempty"`
	Function apiToolCallFn `json:"function"`
}

// apiUsage holds token consumption data.
type apiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Complete sends a non-streaming completion request to OpenRouter.
func (o *OpenRouter) Complete(ctx context.Context, req provider.CompletionRequest) (provider.CompletionResponse, error) {
	apiReq := o.buildRequest(req, false)

	resp, err := o.doRequest(ctx, apiReq)
	if err != nil {
		return provider.CompletionResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return provider.CompletionResponse{}, mapHTTPError(resp.StatusCode, resp.Body)
	}

	var apiResp apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return provider.CompletionResponse{}, fmt.Errorf("openrouter: decoding response: %w", err)
	}

	if apiResp.Error.Message != "" {
		return provider.CompletionResponse{}, fmt.Errorf("openrouter: %s", apiResp.Error.Message)
	}

	return convertResponse(apiResp), nil
}

// Stream sends a streaming completion request to OpenRouter.
// Connection errors are returned directly. Mid-stream errors are
// delivered via StreamChunk.Err on the returned channel.
func (o *OpenRouter) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	apiReq := o.buildRequest(req, true)

	resp, err := o.doRequest(ctx, apiReq)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		return nil, mapHTTPError(resp.StatusCode, resp.Body)
	}

	ch := make(chan provider.StreamChunk, 8)
	go func() {
		defer func() { _ = resp.Body.Close() }()
		defer close(ch)
		parseSSE(resp.Body, ch)
	}()

	return ch, nil
}

// ContextWindowSize returns the context window size for the configured model.
// Config override takes precedence over the built-in lookup table.
func (o *OpenRouter) ContextWindowSize() int {
	if o.config.ContextWindow > 0 {
		return o.config.ContextWindow
	}
	return lookupContextWindow(o.config.resolvedModel())
}

// ModelName returns the resolved model identifier.
func (o *OpenRouter) ModelName() string {
	return o.config.resolvedModel()
}

// HealthCheck performs an active health probe by sending a minimal
// completion request (max_tokens=1) to verify connectivity.
func (o *OpenRouter) HealthCheck(ctx context.Context) error {
	req := provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "ping"},
		},
		MaxTokens: 1,
	}
	_, err := o.Complete(ctx, req)
	return err
}

// buildRequest converts a provider.CompletionRequest into an apiRequest.
func (o *OpenRouter) buildRequest(req provider.CompletionRequest, stream bool) apiRequest {
	ar := apiRequest{
		Model:       o.config.resolvedModel(),
		Messages:    convertMessages(req.Messages),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stop:        req.Stop,
		Stream:      stream,
	}

	if len(req.Tools) > 0 {
		ar.Tools = convertTools(req.Tools)
	}

	return ar
}

// doRequest sends an API request and returns the raw HTTP response.
func (o *OpenRouter) doRequest(ctx context.Context, apiReq apiRequest) (*http.Response, error) {
	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("openrouter: marshaling request: %w", err)
	}

	url := o.config.BaseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openrouter: creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.config.APIKey)

	if o.config.Referer != "" {
		httpReq.Header.Set("HTTP-Referer", o.config.Referer)
	}
	if o.config.Title != "" {
		httpReq.Header.Set("X-Title", o.config.Title)
	}

	resp, err := o.client.Do(httpReq)
	if err != nil {
		// Wrap transport errors with ErrProviderDown so the failover chain
		// treats network failures as retryable.
		return nil, fmt.Errorf("openrouter: sending request: %w", provider.ErrProviderDown)
	}

	return resp, nil
}

// convertMessages converts provider messages to API messages.
func convertMessages(msgs []provider.LLMMessage) []apiMessage {
	out := make([]apiMessage, len(msgs))
	for i, m := range msgs {
		am := apiMessage{
			Role:       string(m.Role),
			Content:    m.Content,
			Name:       m.Name,
			ToolCallID: m.ToolID,
		}
		if len(m.ToolCalls) > 0 {
			am.ToolCalls = make([]apiToolCall, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				am.ToolCalls[j] = apiToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: apiToolCallFn{
						Name:      tc.Name,
						Arguments: string(tc.Arguments),
					},
				}
			}
		}
		out[i] = am
	}
	return out
}

// convertTools converts provider tool definitions to API tools.
func convertTools(tools []provider.ToolDefinition) []apiTool {
	out := make([]apiTool, len(tools))
	for i, t := range tools {
		out[i] = apiTool{
			Type: "function",
			Function: apiFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		}
	}
	return out
}

// convertResponse converts an API response to a provider.CompletionResponse.
func convertResponse(resp apiResponse) provider.CompletionResponse {
	cr := provider.CompletionResponse{
		Usage: provider.TokenUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
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
