// Package openairesponses provides an OpenAI Responses API provider module
// using native WebSocket streaming for persistent, low-latency connections.
package openairesponses

import (
	"fmt"
	"net/url"
	"time"
)

// Config holds the configuration for the OpenAI Responses API provider.
type Config struct {
	APIKey        string            `yaml:"api_key"`
	APIKeyEnv     string            `yaml:"api_key_env"`
	Model         string            `yaml:"model"`
	ContextWindow int               `yaml:"context_window"`
	MaxTokens     int               `yaml:"max_output_tokens"`
	WSEndpoint    string            `yaml:"ws_endpoint"`
	Headers       map[string]string `yaml:"headers"`
	DialTimeout   time.Duration     `yaml:"dial_timeout"`
	ConnMaxAge    time.Duration     `yaml:"conn_max_age"`
}

// defaults sets default values for unset fields.
func (c *Config) defaults() {
	if c.WSEndpoint == "" {
		c.WSEndpoint = "wss://api.openai.com/v1/responses"
	}
	if c.ContextWindow == 0 {
		c.ContextWindow = 128_000
	}
	if c.DialTimeout == 0 {
		c.DialTimeout = 10 * time.Second
	}
	if c.ConnMaxAge == 0 {
		c.ConnMaxAge = 55 * time.Minute
	}
}

// validate returns an error if required fields are missing or invalid.
func (c *Config) validate() error {
	if c.APIKey == "" && c.APIKeyEnv == "" {
		return fmt.Errorf("provider.openai_responses: one of api_key or api_key_env is required")
	}
	if c.Model == "" {
		return fmt.Errorf("provider.openai_responses: model is required")
	}
	u, err := url.Parse(c.WSEndpoint)
	if err != nil {
		return fmt.Errorf("provider.openai_responses: ws_endpoint is not a valid URL: %w", err)
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return fmt.Errorf("provider.openai_responses: ws_endpoint scheme must be ws or wss, got %q", u.Scheme)
	}
	if c.ContextWindow < 0 {
		return fmt.Errorf("provider.openai_responses: context_window must not be negative")
	}
	if c.MaxTokens < 0 {
		return fmt.Errorf("provider.openai_responses: max_output_tokens must not be negative")
	}
	if c.DialTimeout < 0 {
		return fmt.Errorf("provider.openai_responses: dial_timeout must not be negative")
	}
	if c.ConnMaxAge < 0 {
		return fmt.Errorf("provider.openai_responses: conn_max_age must not be negative")
	}
	return nil
}
