// Package anthropic implements the provider.anthropic module, bridging sclaw
// to the Anthropic Messages API for LLM completions and streaming.
package anthropic

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	sdkanthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/security"
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
	_ core.Stopper           = (*Anthropic)(nil)
	_ provider.Provider      = (*Anthropic)(nil)
	_ provider.HealthChecker = (*Anthropic)(nil)
)

// Anthropic is the provider.anthropic module. It implements provider.Provider
// and provider.HealthChecker using the Anthropic Messages API.
type Anthropic struct {
	config         Config
	client         *sdkanthropic.Client
	logger         *slog.Logger
	contextWindow  int
	apiKeyResolved bool
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

	// Resolve API key: api_key > api_key_env > ANTHROPIC_API_KEY fallback.
	apiKey, err := a.resolveAPIKey()
	if err != nil {
		return err
	}
	a.apiKeyResolved = apiKey != ""

	// Register the resolved key with the credential store for redaction/audit.
	if a.apiKeyResolved {
		if svc, ok := ctx.GetService("security.credentials"); ok {
			if store, ok := svc.(*security.CredentialStore); ok {
				store.Set("anthropic_api_key", apiKey)
			}
		}
	}

	// Build SDK client options.
	var opts []option.RequestOption
	if a.apiKeyResolved {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	if a.config.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(a.config.BaseURL))
	}
	// Disable SDK-level retries — the provider chain handles retries.
	opts = append(opts, option.WithMaxRetries(0))

	// Use a custom HTTP client with explicit transport timeouts.
	httpClient := &http.Client{
		Transport: &http.Transport{
			ResponseHeaderTimeout: a.config.Timeout,
			TLSHandshakeTimeout:   10 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}
	opts = append(opts, option.WithHTTPClient(httpClient))

	client := sdkanthropic.NewClient(opts...)
	a.client = &client

	// Resolve context window.
	a.contextWindow = a.config.contextWindowForModel()
	if a.config.ContextWindow == 0 {
		a.logger.Info("resolved context window from default",
			"model", a.config.Model,
			"context_window", a.contextWindow,
		)
	}

	// Register this provider for cross-module discovery (e.g. multiagent orchestrator).
	ctx.RegisterService("provider.anthropic", a)

	// Clear the raw API key from config to avoid accidental logging.
	a.config.APIKey = ""

	return nil
}

// resolveAPIKey resolves the API key from config, env var name, or default env var.
func (a *Anthropic) resolveAPIKey() (string, error) {
	// Explicit api_key in config takes highest precedence.
	if a.config.APIKey != "" {
		return a.config.APIKey, nil
	}

	// api_key_env: read from the named environment variable.
	if a.config.APIKeyEnv != "" {
		if v, ok := os.LookupEnv(a.config.APIKeyEnv); ok && v != "" {
			return v, nil
		}
		return "", fmt.Errorf("provider.anthropic: env var %q is empty or unset", a.config.APIKeyEnv)
	}

	// Fallback: default ANTHROPIC_API_KEY environment variable.
	if v, ok := os.LookupEnv("ANTHROPIC_API_KEY"); ok {
		return v, nil
	}

	return "", nil
}

// Validate implements core.Validator.
func (a *Anthropic) Validate() error {
	if a.config.Model == "" {
		return errors.New("provider.anthropic: model must not be empty")
	}
	if a.client == nil {
		return errors.New("provider.anthropic: client not initialized (Provision not called)")
	}
	if !a.apiKeyResolved {
		return errors.New("provider.anthropic: no API key provided (set api_key, api_key_env, or ANTHROPIC_API_KEY)")
	}
	return nil
}

// Stop implements core.Stopper.
// The SDK client uses Go's default HTTP transport pooling which is
// cleaned up by the runtime. No explicit teardown is required.
func (a *Anthropic) Stop(_ context.Context) error {
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
