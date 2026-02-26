// Package openaicompat provides an OpenAI-compatible LLM provider module.
// It works with any API that implements the OpenAI chat completions interface
// (Mistral, Groq, DeepSeek, Together, vLLM, LiteLLM, etc.) via a configurable base_url.
package openaicompat

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/provider"
	"gopkg.in/yaml.v3"
)

func init() {
	core.RegisterModule(&Provider{})
}

// Provider is an OpenAI-compatible LLM provider.
type Provider struct {
	config Config
	client *http.Client
	logger *slog.Logger
}

// ModuleInfo implements core.Module.
func (p *Provider) ModuleInfo() core.ModuleInfo {
	return core.ModuleInfo{
		ID:  "provider.openai_compatible",
		New: func() core.Module { return &Provider{} },
	}
}

// Configure implements core.Configurable.
func (p *Provider) Configure(node *yaml.Node) error {
	if err := node.Decode(&p.config); err != nil {
		return err
	}
	p.config.defaults()
	return nil
}

// Provision implements core.Provisioner.
func (p *Provider) Provision(ctx *core.AppContext) error {
	p.logger = ctx.Logger

	// Resolve API key: api_key_env takes precedence (reads from environment),
	// falling back to the literal api_key value from config.
	if p.config.APIKeyEnv != "" {
		if v, ok := os.LookupEnv(p.config.APIKeyEnv); ok && v != "" {
			p.config.APIKey = v
		} else {
			return fmt.Errorf("provider.openai_compatible: env var %q is empty or unset", p.config.APIKeyEnv)
		}
	}

	// Use a transport with response-header timeout instead of a global client timeout.
	// A global timeout kills long-running SSE streams; per-request context handles cancellation.
	p.client = &http.Client{
		Transport: &http.Transport{
			ResponseHeaderTimeout: p.config.Timeout,
			TLSHandshakeTimeout:   10 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}

	// Register this provider as an individual service so that an external
	// orchestrator (e.g. multiagent) can discover providers and build the
	// failover Chain itself. This avoids each provider creating its own
	// single-entry chain and clobbering a shared "provider.chain" key.
	ctx.RegisterService("provider.openai_compatible", p)
	return nil
}

// Validate implements core.Validator.
func (p *Provider) Validate() error {
	return p.config.validate()
}

// Complete implements provider.Provider.
func (p *Provider) Complete(ctx context.Context, req provider.CompletionRequest) (provider.CompletionResponse, error) {
	oaiReq := buildRequest(p.config.Model, p.config.MaxTokens, req, false)

	resp, err := p.doRequest(ctx, oaiReq)
	if err != nil {
		return provider.CompletionResponse{}, err
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close

	if resp.StatusCode != http.StatusOK {
		return provider.CompletionResponse{}, handleErrorResponse(resp)
	}

	var oaiResp oaiResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return provider.CompletionResponse{}, fmt.Errorf("decode response: %w", err)
	}

	return parseResponse(oaiResp), nil
}

// Stream implements provider.Provider.
func (p *Provider) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	oaiReq := buildRequest(p.config.Model, p.config.MaxTokens, req, true)

	resp, err := p.doRequest(ctx, oaiReq)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close() //nolint:errcheck // best-effort close
		return nil, handleErrorResponse(resp)
	}

	// Increase scanner buffer to 1 MiB to handle large SSE lines (e.g. long tool call arguments).
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	ch := p.parseSSEStream(ctx, scanner)

	// Wrap to ensure body gets closed when stream ends.
	// Select on ctx.Done() to avoid goroutine leak if consumer abandons the channel.
	out := make(chan provider.StreamChunk, 16)
	go func() {
		defer close(out)
		defer resp.Body.Close() //nolint:errcheck // best-effort close
		for chunk := range ch {
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, nil
}

// ContextWindowSize implements provider.Provider.
func (p *Provider) ContextWindowSize() int {
	return p.config.ContextWindow
}

// ModelName implements provider.Provider.
func (p *Provider) ModelName() string {
	return p.config.Model
}

// HealthCheck implements provider.HealthChecker.
// It probes the /models endpoint to check provider availability.
func (p *Provider) HealthCheck(ctx context.Context) error {
	endpoint := p.config.BaseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	for k, v := range p.config.Headers {
		req.Header.Set(k, v)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: health check: %w", provider.ErrProviderDown, err)
	}
	defer resp.Body.Close()               //nolint:errcheck // best-effort close
	_, _ = io.Copy(io.Discard, resp.Body) // drain body

	if resp.StatusCode >= 400 {
		return fmt.Errorf("%w: health check returned HTTP %d", provider.ErrProviderDown, resp.StatusCode)
	}

	return nil
}

// errMissingField returns a validation error for a missing required field.
func errMissingField(field string) error {
	return fmt.Errorf("provider.openai_compatible: %s is required", field)
}

// Compile-time interface assertions.
var (
	_ core.Module            = (*Provider)(nil)
	_ core.Configurable      = (*Provider)(nil)
	_ core.Provisioner       = (*Provider)(nil)
	_ core.Validator         = (*Provider)(nil)
	_ provider.Provider      = (*Provider)(nil)
	_ provider.HealthChecker = (*Provider)(nil)
)
