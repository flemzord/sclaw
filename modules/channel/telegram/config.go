package telegram

import "time"

// Config holds the Telegram channel configuration.
type Config struct {
	Token               string        `yaml:"token"`
	Mode                string        `yaml:"mode"`
	PollingTimeout      int           `yaml:"polling_timeout"`
	WebhookURL          string        `yaml:"webhook_url"`
	WebhookSecret       string        `yaml:"webhook_secret"`
	AllowedUpdates      []string      `yaml:"allowed_updates"`
	AllowUsers          []string      `yaml:"allow_users"`
	AllowGroups         []string      `yaml:"allow_groups"`
	MaxMessageLength    int           `yaml:"max_message_length"`
	StreamFlushInterval time.Duration `yaml:"stream_flush_interval"`
	APIURL              string        `yaml:"api_url"`
}

// defaults applies default values to unset fields.
func (c *Config) defaults() {
	if c.Mode == "" {
		c.Mode = "polling"
	}
	if c.PollingTimeout == 0 {
		c.PollingTimeout = 30
	}
	if c.AllowedUpdates == nil {
		c.AllowedUpdates = []string{"message", "edited_message", "channel_post"}
	}
	if c.MaxMessageLength == 0 {
		c.MaxMessageLength = 4096
	}
	if c.StreamFlushInterval <= 0 {
		c.StreamFlushInterval = time.Second
	}
	if c.APIURL == "" {
		c.APIURL = "https://api.telegram.org"
	}
}
