// Package anthropic implements the provider.anthropic module, bridging sclaw
// to the Anthropic Messages API for LLM completions and streaming.
package anthropic

import (
	"errors"
	"log/slog"
	"os"

	sdkanthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/provider"
	"gopkg.in/yaml.v3"
)

func init() {
	core.RegisterModule(&Anthropic{})
}

// Interface guards.
var (
	_ core.Module            = (*Anthropic)(nil)
	_ core.Configurable      = (*Anthropic)(nil)
	_ core.Provisioner       = (*Anthropic)(nil)
	_ core.Validator         = (*Anthropic)(nil)
	_ provider.Provider      = (*Anthropic)(nil)
	_ provider.HealthChecker = (*Anthropic)(nil)
)

// Anthropic is the provider.anthropic module. It implements provider.Provider
// and provider.HealthChecker using the Anthropic Messages API.
type Anthropic struct {
	config        Config
	client        *sdkanthropic.Client
	logger        *slog.Logger
	contextWindow int
}

// ModuleInfo implements core.Module.
func (a *Anthropic) ModuleInfo() core.ModuleInfo {
	return core.ModuleInfo{
		ID:  "provider.anthropic",
		New: func() core.Module { return &Anthropic{} },
	}
}

// Configure implements core.Configurable.
func (a *Anthropic) Configure(node *yaml.Node) error {
	if err := node.Decode(&a.config); err != nil {
		return err
	}
	a.config.defaults()
	return nil
}

// Provision implements core.Provisioner.
func (a *Anthropic) Provision(ctx *core.AppContext) error {
	a.logger = ctx.Logger

	// Resolve API key: config takes precedence over environment variable.
	apiKey := a.config.APIKey
	if apiKey == "" {
		if envKey, ok := os.LookupEnv("ANTHROPIC_API_KEY"); ok {
			apiKey = envKey
		}
	}

	// Build SDK client options.
	var opts []option.RequestOption
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	if a.config.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(a.config.BaseURL))
	}
	// Disable SDK-level retries — the provider chain handles retries.
	opts = append(opts, option.WithMaxRetries(0))

	client := sdkanthropic.NewClient(opts...)
	a.client = &client

	// Resolve context window.
	a.contextWindow = a.config.contextWindowForModel()
	if a.config.ContextWindow == 0 {
		a.logger.Info("resolved context window from model prefix",
			"model", a.config.Model,
			"context_window", a.contextWindow,
		)
	}

	return nil
}

// Validate implements core.Validator.
func (a *Anthropic) Validate() error {
	if a.config.Model == "" {
		return errors.New("provider.anthropic: model must not be empty")
	}
	if a.client == nil {
		return errors.New("provider.anthropic: client not initialized (Provision not called)")
	}
	return nil
}

// ContextWindowSize implements provider.Provider.
func (a *Anthropic) ContextWindowSize() int {
	return a.contextWindow
}

// ModelName implements provider.Provider.
func (a *Anthropic) ModelName() string {
	return a.config.Model
}
