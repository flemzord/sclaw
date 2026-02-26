package openaicompat

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Config holds the configuration for an OpenAI-compatible provider.
type Config struct {
	BaseURL       string            `yaml:"base_url"`
	APIKey        string            `yaml:"api_key"`
	APIKeyEnv     string            `yaml:"api_key_env"`
	Model         string            `yaml:"model"`
	ContextWindow int               `yaml:"context_window"`
	MaxTokens     int               `yaml:"max_tokens"`
	Headers       map[string]string `yaml:"headers"`
	Timeout       time.Duration     `yaml:"timeout"`
}

// defaults sets default values for unset fields.
func (c *Config) defaults() {
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	if c.ContextWindow == 0 {
		c.ContextWindow = 4096
	}
	if c.BaseURL != "" {
		c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	}
}

// validate returns an error if required fields are missing.
func (c *Config) validate() error {
	if c.BaseURL == "" {
		return errMissingField("base_url")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return fmt.Errorf("provider.openai_compatible: base_url is not a valid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("provider.openai_compatible: base_url scheme must be http or https, got %q", u.Scheme)
	}
	if c.APIKey == "" && c.APIKeyEnv == "" {
		return fmt.Errorf("provider.openai_compatible: one of api_key or api_key_env is required")
	}
	if c.Model == "" {
		return errMissingField("model")
	}
	if c.ContextWindow < 0 {
		return fmt.Errorf("provider.openai_compatible: context_window must not be negative")
	}
	if c.MaxTokens < 0 {
		return fmt.Errorf("provider.openai_compatible: max_tokens must not be negative")
	}
	return nil
}
