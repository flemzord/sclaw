package anthropic

import "strings"

// Config holds the YAML-decoded configuration for the Anthropic provider.
type Config struct {
	APIKey        string `yaml:"api_key"`
	Model         string `yaml:"model"`
	BaseURL       string `yaml:"base_url"`
	MaxTokens     int    `yaml:"max_tokens"`
	ContextWindow int    `yaml:"context_window"`
}

// defaults fills in zero-value fields with sensible defaults.
func (c *Config) defaults() {
	if c.Model == "" {
		c.Model = "claude-sonnet-4-5-20250929"
	}
	if c.MaxTokens == 0 {
		c.MaxTokens = 4096
	}
}

// contextWindowForModel returns the context window size for the configured model.
// If an explicit override is set, it is returned directly.
// Otherwise, the model name is matched by prefix against known families.
func (c *Config) contextWindowForModel() int {
	if c.ContextWindow > 0 {
		return c.ContextWindow
	}
	return lookupContextWindow(c.Model)
}

// contextWindows maps model name prefixes to their context window sizes in tokens.
var contextWindows = []struct {
	prefix string
	tokens int
}{
	{"claude-4", 200_000},
	{"claude-3", 200_000},
}

// defaultContextWindow is used when no prefix matches.
const defaultContextWindow = 200_000

// lookupContextWindow resolves a model name to its context window via prefix matching.
func lookupContextWindow(model string) int {
	for _, entry := range contextWindows {
		if strings.HasPrefix(model, entry.prefix) {
			return entry.tokens
		}
	}
	return defaultContextWindow
}
