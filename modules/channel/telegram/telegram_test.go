package telegram

import "testing"

func TestConfigValidate_InvalidToken(t *testing.T) {
	cfg := Config{Token: "invalid-token", Mode: "polling"}
	cfg.defaults()
	if err := cfg.validate(); err == nil {
		t.Error("validate() should reject invalid token format")
	}
}

func TestConfigValidate_ValidToken(t *testing.T) {
	cfg := Config{Token: "123456:ABC-DEF_ghijk", Mode: "polling"}
	cfg.defaults()
	if err := cfg.validate(); err != nil {
		t.Errorf("validate() unexpected error: %v", err)
	}
}

func TestConfigValidate_InvalidAPIURL(t *testing.T) {
	cfg := Config{Token: "123:abc", APIURL: "not-a-url"}
	cfg.defaults()
	if err := cfg.validate(); err == nil {
		t.Error("validate() should reject invalid API URL")
	}
}

func TestConfigValidate_PollingTimeoutBounds(t *testing.T) {
	cfg := Config{Token: "123:abc", PollingTimeout: 60}
	cfg.defaults()
	if err := cfg.validate(); err == nil {
		t.Error("validate() should reject polling_timeout > 50")
	}
}

func TestConfigValidate_MaxMessageLengthBounds(t *testing.T) {
	cfg := Config{Token: "123:abc", MaxMessageLength: 10000}
	cfg.defaults()
	if err := cfg.validate(); err == nil {
		t.Error("validate() should reject max_message_length > 4096")
	}
}

func TestConfigValidate_StreamFlushIntervalBounds(t *testing.T) {
	cfg := Config{Token: "123:abc", StreamFlushInterval: 1}
	cfg.defaults() // won't override since > 0
	if err := cfg.validate(); err == nil {
		t.Error("validate() should reject stream_flush_interval < 100ms")
	}
}
