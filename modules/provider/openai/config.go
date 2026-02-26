package openai

import (
	"fmt"
	"time"
)

// Config holds the configuration for the OpenAI provider module.
type Config struct {
	APIKey        string   `yaml:"api_key"`
	Model         string   `yaml:"model"`
	BaseURL       string   `yaml:"base_url"`
	MaxTokens     int      `yaml:"max_tokens"`
	Temperature   *float64 `yaml:"temperature"`
	TopP          *float64 `yaml:"top_p"`
	Timeout       string   `yaml:"timeout"`
	ContextWindow int      `yaml:"context_window"`
}

// defaults fills zero-valued fields with sensible defaults.
func (c *Config) defaults() {
	if c.BaseURL == "" {
		c.BaseURL = "https://api.openai.com/v1"
	}
	if c.Timeout == "" {
		c.Timeout = "30s"
	}
}

// parsedTimeout returns the timeout as a time.Duration.
// Assumes the value has been validated by validateTimeout.
func (c *Config) parsedTimeout() time.Duration {
	d, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return 30 * time.Second
	}
	return d
}

// validateTimeout checks that the timeout string is a valid Go duration.
func (c *Config) validateTimeout() error {
	_, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return fmt.Errorf("provider.openai: invalid timeout %q: %w", c.Timeout, err)
	}
	return nil
}

// knownContextWindows maps model names to their maximum context window size
// in tokens. Used when context_window is not explicitly set in config.
var knownContextWindows = map[string]int{
	"gpt-3.5-turbo":       16385,
	"gpt-4":               8192,
	"gpt-4-turbo":         128000,
	"gpt-4-turbo-preview": 128000,
	"gpt-4o":              128000,
	"gpt-4o-mini":         128000,
	"gpt-4.1":             1048576,
	"gpt-4.1-mini":        1048576,
	"gpt-4.1-nano":        1048576,
	"o1":                  200000,
	"o1-mini":             128000,
	"o1-preview":          128000,
	"o3":                  200000,
	"o3-mini":             200000,
	"o4-mini":             200000,
}
