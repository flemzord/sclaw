package openaicompat

import "time"

// Config holds the configuration for an OpenAI-compatible provider.
type Config struct {
	BaseURL       string            `yaml:"base_url"`
	APIKey        string            `yaml:"api_key"`
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
}

// validate returns an error if required fields are missing.
func (c *Config) validate() error {
	if c.BaseURL == "" {
		return errMissingField("base_url")
	}
	if c.APIKey == "" {
		return errMissingField("api_key")
	}
	if c.Model == "" {
		return errMissingField("model")
	}
	return nil
}
