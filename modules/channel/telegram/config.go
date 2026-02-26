package telegram

import (
	"fmt"
	"net/url"
	"regexp"
	"time"
)

// tokenPattern matches the Telegram bot token format: <digits>:<alphanum+dash>.
var tokenPattern = regexp.MustCompile(`^\d+:[A-Za-z0-9_-]+$`)

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

// validate checks configuration field constraints beyond basic presence checks.
// It is called from Telegram.Validate after defaults have been applied.
func (c *Config) validate() error {
	if c.Token != "" && !tokenPattern.MatchString(c.Token) {
		return fmt.Errorf("telegram: token format invalid (expected <bot_id>:<hash>)")
	}

	if c.APIURL != "" {
		u, err := url.Parse(c.APIURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("telegram: api_url must be a valid http/https URL, got %q", c.APIURL)
		}
	}

	if c.PollingTimeout < 0 || c.PollingTimeout > 50 {
		return fmt.Errorf("telegram: polling_timeout must be 0-50, got %d", c.PollingTimeout)
	}

	if c.MaxMessageLength < 1 || c.MaxMessageLength > 4096 {
		return fmt.Errorf("telegram: max_message_length must be 1-4096, got %d", c.MaxMessageLength)
	}

	if c.StreamFlushInterval < 100*time.Millisecond || c.StreamFlushInterval > 30*time.Second {
		return fmt.Errorf("telegram: stream_flush_interval must be 100ms-30s, got %s", c.StreamFlushInterval)
	}

	return nil
}
