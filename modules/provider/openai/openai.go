// Package openai implements the provider.openai module, providing OpenAI
// Chat Completions API support with streaming and function calling.
package openai

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/provider"
	"gopkg.in/yaml.v3"
)

func init() {
	core.RegisterModule(&Provider{})
}

// Compile-time interface guards.
var (
	_ provider.Provider      = (*Provider)(nil)
	_ provider.HealthChecker = (*Provider)(nil)
	_ core.Module            = (*Provider)(nil)
	_ core.Configurable      = (*Provider)(nil)
	_ core.Provisioner       = (*Provider)(nil)
	_ core.Validator         = (*Provider)(nil)
)

// Provider implements the OpenAI Chat Completions API as a sclaw provider module.
type Provider struct {
	config        Config
	logger        *slog.Logger
	client        *http.Client
	streamClient  *http.Client
	contextWindow int
}

// ModuleInfo implements core.Module.
func (p *Provider) ModuleInfo() core.ModuleInfo {
	return core.ModuleInfo{
		ID:  "provider.openai",
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

	timeout := p.config.parsedTimeout()

	// Separate clients for non-streaming and streaming requests.
	// http.Client.Timeout is a hard deadline for the entire response body,
	// which would kill long-lived SSE streams. The streaming client uses no
	// timeout; cancellation is handled via context.
	p.client = &http.Client{
		Timeout: timeout,
	}
	p.streamClient = &http.Client{}

	// Resolve context window: explicit config > known model map > 0.
	if p.config.ContextWindow > 0 {
		p.contextWindow = p.config.ContextWindow
	} else if size, ok := knownContextWindows[p.config.Model]; ok {
		p.contextWindow = size
	}

	ctx.RegisterService("provider.openai", p)

	return nil
}

// Validate implements core.Validator.
func (p *Provider) Validate() error {
	if p.config.APIKey == "" {
		return errors.New("provider.openai: api_key is required")
	}
	if p.config.Model == "" {
		return errors.New("provider.openai: model is required")
	}
	if p.contextWindow <= 0 {
		return errors.New("provider.openai: context_window must be set for unknown models")
	}
	if err := p.config.validateTimeout(); err != nil {
		return err
	}
	return nil
}
