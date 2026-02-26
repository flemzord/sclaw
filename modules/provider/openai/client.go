package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/flemzord/sclaw/internal/provider"
)

// maxResponseSize is the maximum response body size (10 MB).
// Protects against OOM from malformed or huge responses.
const maxResponseSize = 10 * 1024 * 1024

// streamChannelBuffer is the buffer size for the streaming channel.
const streamChannelBuffer = 64

// buildChatRequest creates an OpenAI API chat request from a provider
// CompletionRequest, merging request-level overrides with config defaults.
func (p *Provider) buildChatRequest(req provider.CompletionRequest, stream bool) chatRequest {
	cr := chatRequest{
		Model:    p.config.Model,
		Messages: toMessages(req.Messages),
		Stream:   stream,
	}

	if len(req.Tools) > 0 {
		cr.Tools = toTools(req.Tools)
	}

	// Request-level overrides take precedence over config defaults.
	switch {
	case req.MaxTokens > 0:
		cr.MaxTokens = req.MaxTokens
	case p.config.MaxTokens > 0:
		cr.MaxTokens = p.config.MaxTokens
	}

	switch {
	case req.Temperature != nil:
		cr.Temperature = req.Temperature
	case p.config.Temperature != nil:
		cr.Temperature = p.config.Temperature
	}

	switch {
	case req.TopP != nil:
		cr.TopP = req.TopP
	case p.config.TopP != nil:
		cr.TopP = p.config.TopP
	}

	if len(req.Stop) > 0 {
		cr.Stop = req.Stop
	}

	if stream {
		cr.StreamOptions = &streamOpts{IncludeUsage: true}
	}

	return cr
}

// newHTTPRequest creates an authenticated HTTP request for the OpenAI API.
func (p *Provider) newHTTPRequest(ctx context.Context, path string, payload any) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	url := p.config.BaseURL + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.config.APIKey)

	return httpReq, nil
}

// doPost sends a POST request and returns the response body and status code.
// The response body is limited to maxResponseSize bytes.
func (p *Provider) doPost(ctx context.Context, path string, payload any) ([]byte, int, error) {
	httpReq, err := p.newHTTPRequest(ctx, path, payload)
	if err != nil {
		return nil, 0, err
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, 0, mapConnectionError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("openai: read response: %w", err)
	}

	return body, resp.StatusCode, nil
}

// Complete sends a non-streaming completion request and returns the full response.
func (p *Provider) Complete(ctx context.Context, req provider.CompletionRequest) (provider.CompletionResponse, error) {
	cr := p.buildChatRequest(req, false)

	body, statusCode, err := p.doPost(ctx, "/chat/completions", cr)
	if err != nil {
		return provider.CompletionResponse{}, err
	}

	if httpErr := mapHTTPError(statusCode, body); httpErr != nil {
		return provider.CompletionResponse{}, httpErr
	}

	var resp chatResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return provider.CompletionResponse{}, fmt.Errorf("openai: unmarshal response: %w", err)
	}

	return fromResponse(&resp), nil
}

// Stream sends a streaming completion request and returns a channel of chunks.
// Initial connection errors are returned directly. Mid-stream errors are
// delivered via StreamChunk.Err.
func (p *Provider) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	cr := p.buildChatRequest(req, true)

	httpReq, err := p.newHTTPRequest(ctx, "/chat/completions", cr)
	if err != nil {
		return nil, err
	}

	resp, err := p.streamClient.Do(httpReq)
	if err != nil {
		return nil, mapConnectionError(err)
	}

	// Check for HTTP errors before starting the stream.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		return nil, mapHTTPError(resp.StatusCode, body)
	}

	ch := make(chan provider.StreamChunk, streamChannelBuffer)
	go readStream(ctx, resp.Body, ch)

	return ch, nil
}

// HealthCheck validates the provider is functional by sending a minimal
// 1-token completion. This tests the full path: authentication, model
// access, and quota.
func (p *Provider) HealthCheck(ctx context.Context) error {
	req := provider.CompletionRequest{
		Messages: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "hi"},
		},
		MaxTokens: 1,
	}
	_, err := p.Complete(ctx, req)
	return err
}

// ContextWindowSize returns the maximum context window in tokens.
func (p *Provider) ContextWindowSize() int {
	return p.contextWindow
}

// ModelName returns the configured model identifier.
func (p *Provider) ModelName() string {
	return p.config.Model
}
