package openrouter

import "time"

const (
	defaultBaseURL = "https://openrouter.ai/api/v1"
	defaultTimeout = "120s"
)

// Config holds the YAML configuration for the OpenRouter provider module.
type Config struct {
	// APIKey is the OpenRouter API key (required). Typically sk-or-v1-...
	APIKey string `yaml:"api_key"`

	// Model is the model identifier (required). "auto" is mapped to "openrouter/auto".
	Model string `yaml:"model"`

	// BaseURL is the OpenRouter API base URL.
	// Default: "https://openrouter.ai/api/v1"
	BaseURL string `yaml:"base_url"`

	// Referer is sent as the HTTP-Referer header (optional).
	Referer string `yaml:"referer"`

	// Title is sent as the X-Title header (optional).
	Title string `yaml:"title"`

	// Timeout is the HTTP request timeout as a duration string.
	// Default: "120s"
	Timeout string `yaml:"timeout"`

	// ContextWindow overrides automatic context window detection.
	// 0 means use the built-in lookup table.
	ContextWindow int `yaml:"context_window"`
}

// defaults fills in zero-value fields with sensible defaults.
func (c *Config) defaults() {
	if c.BaseURL == "" {
		c.BaseURL = defaultBaseURL
	}
	if c.Timeout == "" {
		c.Timeout = defaultTimeout
	}
}

// resolvedModel returns the canonical model name.
// "auto" is mapped to "openrouter/auto".
func (c *Config) resolvedModel() string {
	if c.Model == "auto" {
		return "openrouter/auto"
	}
	return c.Model
}

// parsedTimeout parses Timeout as a time.Duration.
func (c *Config) parsedTimeout() (time.Duration, error) {
	return time.ParseDuration(c.Timeout)
}
