package gateway

import "time"

// Config holds HTTP gateway configuration.
type Config struct {
	Bind            string                      `yaml:"bind"`
	Auth            AuthConfig                  `yaml:"auth"`
	Webhooks        map[string]WebhookSourceCfg `yaml:"webhooks"`
	ReadTimeout     time.Duration               `yaml:"read_timeout"`
	WriteTimeout    time.Duration               `yaml:"write_timeout"`
	ShutdownTimeout time.Duration               `yaml:"shutdown_timeout"`
}

// defaults fills zero values with sensible defaults.
func (c *Config) defaults() {
	if c.Bind == "" {
		c.Bind = "127.0.0.1:8080"
	}
	if c.ReadTimeout <= 0 {
		c.ReadTimeout = 10 * time.Second
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = 30 * time.Second
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 5 * time.Second
	}
}

// AuthConfig configures authentication for admin endpoints.
type AuthConfig struct {
	BearerToken string `yaml:"bearer_token"`
	BasicUser   string `yaml:"basic_user"`
	BasicPass   string `yaml:"basic_pass"`
}

// IsConfigured returns true if any auth method is configured.
func (a AuthConfig) IsConfigured() bool {
	return a.BearerToken != "" || (a.BasicUser != "" && a.BasicPass != "")
}

// WebhookSourceCfg holds per-source webhook configuration.
type WebhookSourceCfg struct {
	Secret string `yaml:"secret"`
}
