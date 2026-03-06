package openairesponses

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/provider"
	"gopkg.in/yaml.v3"
)

func init() {
	core.RegisterModule(&Provider{})
}

// Provider implements the OpenAI Responses API over WebSocket.
type Provider struct {
	config Config
	conn   *connManager
	logger *slog.Logger
	client *http.Client // for health checks only
}

// ModuleInfo implements core.Module.
func (p *Provider) ModuleInfo() core.ModuleInfo {
	return core.ModuleInfo{
		ID:  "provider.openai_responses",
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

	// Resolve API key: api_key_env takes precedence.
	if p.config.APIKeyEnv != "" {
		if v, ok := os.LookupEnv(p.config.APIKeyEnv); ok && v != "" {
			p.config.APIKey = v
		} else {
			return fmt.Errorf("provider.openai_responses: env var %q is empty or unset", p.config.APIKeyEnv)
		}
	}

	p.conn = newConnManager(p.config, p.logger)

	// HTTP client for health checks only.
	p.client = &http.Client{
		Timeout: 10 * time.Second,
	}

	ctx.RegisterService("provider.openai_responses", p)
	return nil
}

// Validate implements core.Validator.
func (p *Provider) Validate() error {
	return p.config.validate()
}

// Stop implements core.Stopper.
func (p *Provider) Stop(_ context.Context) error {
	return p.conn.Close()
}

// Complete implements provider.Provider.
// It sends a response.create event and accumulates all chunks into a single response.
func (p *Provider) Complete(ctx context.Context, req provider.CompletionRequest) (provider.CompletionResponse, error) {
	ch, err := p.doStream(ctx, req)
	if err != nil {
		return provider.CompletionResponse{}, err
	}

	var resp provider.CompletionResponse
	var contentBuilder strings.Builder

	for chunk := range ch {
		if chunk.Err != nil {
			return provider.CompletionResponse{}, chunk.Err
		}
		if chunk.Content != "" {
			contentBuilder.WriteString(chunk.Content)
		}
		if len(chunk.ToolCalls) > 0 {
			resp.ToolCalls = append(resp.ToolCalls, chunk.ToolCalls...)
		}
		if chunk.FinishReason != "" {
			resp.FinishReason = chunk.FinishReason
		}
		if chunk.Usage != nil {
			resp.Usage = *chunk.Usage
		}
	}

	resp.Content = contentBuilder.String()
	return resp, nil
}

// Stream implements provider.Provider.
// It sends a response.create event and returns a channel of streaming chunks.
func (p *Provider) Stream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	return p.doStream(ctx, req)
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
// It probes the /models REST endpoint (same pattern as openai_compatible).
func (p *Provider) HealthCheck(ctx context.Context) error {
	// Derive the REST base URL from the WebSocket endpoint.
	baseURL := wsToHTTP(p.config.WSEndpoint)
	endpoint := baseURL + "/models"

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

// doStream acquires a fresh WebSocket connection, sends the response.create
// event, and returns the stream channel. The connection is always closed when
// the stream completes (coder/websocket closes on any context cancellation,
// making connection reuse impractical).
func (p *Provider) doStream(ctx context.Context, req provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	conn, err := p.conn.getConn(ctx)
	if err != nil {
		return nil, err
	}

	event := buildClientEvent(p.config, req)
	payload, err := json.Marshal(event)
	if err != nil {
		p.conn.invalidate()
		return nil, fmt.Errorf("marshal response.create: %w", err)
	}

	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		p.conn.invalidate()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: WebSocket write: %w", provider.ErrProviderDown, err)
	}

	ch := readLoop(ctx, conn)

	// Wrap to close the connection when the stream completes.
	out := make(chan provider.StreamChunk, 16)
	go func() {
		defer close(out)
		defer p.conn.invalidate()
		for chunk := range ch {
			select {
			case out <- chunk:
			case <-ctx.Done():
				select {
				case out <- provider.StreamChunk{Err: ctx.Err()}:
				default:
				}
				return
			}
		}
	}()

	return out, nil
}

// wsToHTTP converts a WebSocket URL to its HTTP equivalent for REST calls.
func wsToHTTP(wsURL string) string {
	u := strings.TrimSuffix(wsURL, "/responses")
	u = strings.TrimSuffix(u, "/")
	u = strings.Replace(u, "wss://", "https://", 1)
	u = strings.Replace(u, "ws://", "http://", 1)
	return u
}

// Compile-time interface assertions.
var (
	_ core.Module            = (*Provider)(nil)
	_ core.Configurable      = (*Provider)(nil)
	_ core.Provisioner       = (*Provider)(nil)
	_ core.Validator         = (*Provider)(nil)
	_ core.Stopper           = (*Provider)(nil)
	_ provider.Provider      = (*Provider)(nil)
	_ provider.HealthChecker = (*Provider)(nil)
)
