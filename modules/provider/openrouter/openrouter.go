// Package openrouter implements a provider.Provider backed by the OpenRouter API.
// It is the first concrete LLM provider for sclaw, communicating with 200+
// models through an OpenAI-compatible endpoint.
package openrouter

import (
	"fmt"
	"net"
	"net/http"
	"net/url"

	"gopkg.in/yaml.v3"

	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/provider"
)

// Interface guards.
var (
	_ provider.Provider      = (*OpenRouter)(nil)
	_ provider.HealthChecker = (*OpenRouter)(nil)
	_ core.Configurable      = (*OpenRouter)(nil)
	_ core.Provisioner       = (*OpenRouter)(nil)
	_ core.Validator         = (*OpenRouter)(nil)
)

func init() {
	core.RegisterModule(&OpenRouter{})
}

// OpenRouter is a provider.Provider that communicates with the OpenRouter API.
type OpenRouter struct {
	config Config
	client *http.Client
}

// ModuleInfo returns the module metadata for registration.
func (o *OpenRouter) ModuleInfo() core.ModuleInfo {
	return core.ModuleInfo{
		ID:  "provider.openrouter",
		New: func() core.Module { return &OpenRouter{} },
	}
}

// Configure decodes the YAML configuration and applies defaults.
func (o *OpenRouter) Configure(node *yaml.Node) error {
	if err := node.Decode(&o.config); err != nil {
		return fmt.Errorf("openrouter: decoding config: %w", err)
	}
	o.config.defaults()
	return nil
}

// Provision parses the timeout and creates the HTTP client.
// It also registers this provider as a service.
//
// The client uses transport-level timeouts (dial + TLS + response header)
// instead of http.Client.Timeout to avoid killing long-running streams.
// Streaming body reads are governed by context cancellation instead.
func (o *OpenRouter) Provision(ctx *core.AppContext) error {
	timeout, err := o.config.parsedTimeout()
	if err != nil {
		return fmt.Errorf("openrouter: invalid timeout %q: %w", o.config.Timeout, err)
	}

	o.client = &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: timeout}).DialContext,
			TLSHandshakeTimeout:   timeout,
			ResponseHeaderTimeout: timeout,
		},
	}

	ctx.RegisterService("provider.openrouter", o)
	return nil
}

// Validate checks that required configuration fields are set.
func (o *OpenRouter) Validate() error {
	if o.config.APIKey == "" {
		return fmt.Errorf("openrouter: api_key is required")
	}
	if o.config.Model == "" {
		return fmt.Errorf("openrouter: model is required")
	}

	u, err := url.Parse(o.config.BaseURL)
	if err != nil {
		return fmt.Errorf("openrouter: invalid base_url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("openrouter: base_url scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("openrouter: base_url must include a host")
	}

	return nil
}
