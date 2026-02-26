package anthropic

import "time"

// defaultModel is the model used when none is specified.
// Pinned to a dated release for reproducibility; update when a newer
// stable version is validated.
const defaultModel = "claude-sonnet-4-5-20250929"

// defaultContextWindow covers all Claude 3.x and 4.x models (200k tokens).
// If Anthropic introduces a model family with a different window, add an
// explicit lookup table at that point.
const defaultContextWindow = 200_000

// defaultTimeout is the HTTP response-header timeout applied to the
// underlying transport. Streaming responses are not affected once the
// first byte arrives — only the initial connection phase is bounded.
const defaultTimeout = 30 * time.Second

// Config holds the YAML-decoded configuration for the Anthropic provider.
type Config struct {
	APIKey        string        `yaml:"api_key"`
	APIKeyEnv     string        `yaml:"api_key_env"`
	Model         string        `yaml:"model"`
	BaseURL       string        `yaml:"base_url"`
	MaxTokens     int           `yaml:"max_tokens"`
	ContextWindow int           `yaml:"context_window"`
	Timeout       time.Duration `yaml:"timeout"`
}

// defaults fills in zero-value fields with sensible defaults.
func (c *Config) defaults() {
	if c.Model == "" {
		c.Model = defaultModel
	}
	if c.MaxTokens == 0 {
		c.MaxTokens = 4096
	}
	if c.Timeout == 0 {
		c.Timeout = defaultTimeout
	}
}

// contextWindowForModel returns the context window size for the configured model.
// If an explicit override is set, it is returned directly.
func (c *Config) contextWindowForModel() int {
	if c.ContextWindow > 0 {
		return c.ContextWindow
	}
	return defaultContextWindow
}
