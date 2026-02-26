package openai

import (
	"testing"

	"github.com/flemzord/sclaw/internal/core"
	"gopkg.in/yaml.v3"
)

func TestModuleInfo(t *testing.T) {
	p := &Provider{}
	info := p.ModuleInfo()

	if info.ID != "provider.openai" {
		t.Errorf("expected ID provider.openai, got %s", info.ID)
	}
	if info.New == nil {
		t.Fatal("New function must not be nil")
	}

	mod := info.New()
	if _, ok := mod.(*Provider); !ok {
		t.Errorf("New() returned %T, want *Provider", mod)
	}
}

func TestConfigure_Defaults(t *testing.T) {
	p := &Provider{}

	node := yamlNode(t, `
api_key: sk-test
model: gpt-4o
`)
	if err := p.Configure(node); err != nil {
		t.Fatalf("Configure() error: %v", err)
	}

	if p.config.APIKey != "sk-test" {
		t.Errorf("api_key = %q, want sk-test", p.config.APIKey)
	}
	if p.config.Model != "gpt-4o" {
		t.Errorf("model = %q, want gpt-4o", p.config.Model)
	}
	if p.config.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("base_url = %q, want default", p.config.BaseURL)
	}
	if p.config.Timeout != "30s" {
		t.Errorf("timeout = %q, want 30s", p.config.Timeout)
	}
}

func TestConfigure_CustomValues(t *testing.T) {
	p := &Provider{}

	temp := 0.7
	node := yamlNode(t, `
api_key: sk-custom
model: gpt-4-turbo
base_url: https://custom.api.com/v1
max_tokens: 4096
temperature: 0.7
timeout: 60s
context_window: 100000
`)
	if err := p.Configure(node); err != nil {
		t.Fatalf("Configure() error: %v", err)
	}

	if p.config.BaseURL != "https://custom.api.com/v1" {
		t.Errorf("base_url = %q, want custom", p.config.BaseURL)
	}
	if p.config.MaxTokens != 4096 {
		t.Errorf("max_tokens = %d, want 4096", p.config.MaxTokens)
	}
	if p.config.Temperature == nil || *p.config.Temperature != temp {
		t.Errorf("temperature = %v, want %v", p.config.Temperature, temp)
	}
	if p.config.Timeout != "60s" {
		t.Errorf("timeout = %q, want 60s", p.config.Timeout)
	}
	if p.config.ContextWindow != 100000 {
		t.Errorf("context_window = %d, want 100000", p.config.ContextWindow)
	}
}

func TestConfigure_InvalidYAML(t *testing.T) {
	p := &Provider{}
	node := yamlNode(t, `temperature: "not-a-number"`)
	if err := p.Configure(node); err == nil {
		t.Error("expected error for invalid YAML, got nil")
	}
}

func TestProvision_KnownModel(t *testing.T) {
	p := &Provider{
		config: Config{
			APIKey:  "sk-test",
			Model:   "gpt-4o",
			BaseURL: "https://api.openai.com/v1",
			Timeout: "30s",
		},
	}

	ctx := core.NewAppContext(nil, t.TempDir(), t.TempDir())
	if err := p.Provision(ctx); err != nil {
		t.Fatalf("Provision() error: %v", err)
	}

	if p.contextWindow != 128000 {
		t.Errorf("contextWindow = %d, want 128000", p.contextWindow)
	}
	if p.client == nil {
		t.Error("client must not be nil after Provision")
	}
	if p.streamClient == nil {
		t.Error("streamClient must not be nil after Provision")
	}

	svc, ok := ctx.GetService("provider.openai")
	if !ok {
		t.Fatal("service provider.openai not registered")
	}
	if svc != p {
		t.Error("registered service is not the provider instance")
	}
}

func TestProvision_ExplicitContextWindow(t *testing.T) {
	p := &Provider{
		config: Config{
			APIKey:        "sk-test",
			Model:         "custom-model",
			BaseURL:       "https://api.openai.com/v1",
			Timeout:       "30s",
			ContextWindow: 50000,
		},
	}

	ctx := core.NewAppContext(nil, t.TempDir(), t.TempDir())
	if err := p.Provision(ctx); err != nil {
		t.Fatalf("Provision() error: %v", err)
	}

	if p.contextWindow != 50000 {
		t.Errorf("contextWindow = %d, want 50000 (explicit config)", p.contextWindow)
	}
}

func TestValidate_MissingAPIKey(t *testing.T) {
	p := &Provider{
		config:        Config{Model: "gpt-4o", Timeout: "30s"},
		contextWindow: 128000,
	}
	if err := p.Validate(); err == nil {
		t.Error("expected error for missing api_key")
	}
}

func TestValidate_MissingModel(t *testing.T) {
	p := &Provider{
		config:        Config{APIKey: "sk-test", Timeout: "30s"},
		contextWindow: 128000,
	}
	if err := p.Validate(); err == nil {
		t.Error("expected error for missing model")
	}
}

func TestValidate_ZeroContextWindow(t *testing.T) {
	p := &Provider{
		config:        Config{APIKey: "sk-test", Model: "unknown-model", Timeout: "30s"},
		contextWindow: 0,
	}
	if err := p.Validate(); err == nil {
		t.Error("expected error for zero context window")
	}
}

func TestValidate_InvalidTimeout(t *testing.T) {
	p := &Provider{
		config:        Config{APIKey: "sk-test", Model: "gpt-4o", Timeout: "not-a-duration"},
		contextWindow: 128000,
	}
	if err := p.Validate(); err == nil {
		t.Error("expected error for invalid timeout")
	}
}

func TestValidate_OK(t *testing.T) {
	p := &Provider{
		config:        Config{APIKey: "sk-test", Model: "gpt-4o", Timeout: "30s"},
		contextWindow: 128000,
	}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}
}

// yamlNode is a test helper that parses a YAML string into a *yaml.Node.
func yamlNode(t *testing.T, s string) *yaml.Node {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(s), &node); err != nil {
		t.Fatalf("failed to parse test YAML: %v", err)
	}
	// yaml.Unmarshal wraps the document in a document node.
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return node.Content[0]
	}
	return &node
}
