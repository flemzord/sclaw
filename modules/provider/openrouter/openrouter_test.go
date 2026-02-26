package openrouter

import (
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/flemzord/sclaw/internal/core"
)

func TestModuleInfo(t *testing.T) {
	t.Parallel()

	o := &OpenRouter{}
	info := o.ModuleInfo()

	if info.ID != "provider.openrouter" {
		t.Errorf("ID = %q, want %q", info.ID, "provider.openrouter")
	}
	if info.New == nil {
		t.Fatal("New func is nil")
	}
	mod := info.New()
	if _, ok := mod.(*OpenRouter); !ok {
		t.Errorf("New() returned %T, want *OpenRouter", mod)
	}
}

func TestConfigure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		yaml    string
		wantErr bool
		check   func(t *testing.T, c Config)
	}{
		{
			name: "full config",
			yaml: `
api_key: sk-or-test
model: openai/gpt-4o
base_url: https://custom.api/v1
referer: https://myapp.com
title: MyApp
timeout: 60s
context_window: 4096
`,
			check: func(t *testing.T, c Config) {
				t.Helper()
				if c.APIKey != "sk-or-test" {
					t.Errorf("APIKey = %q", c.APIKey)
				}
				if c.Model != "openai/gpt-4o" {
					t.Errorf("Model = %q", c.Model)
				}
				if c.BaseURL != "https://custom.api/v1" {
					t.Errorf("BaseURL = %q", c.BaseURL)
				}
				if c.Referer != "https://myapp.com" {
					t.Errorf("Referer = %q", c.Referer)
				}
				if c.Title != "MyApp" {
					t.Errorf("Title = %q", c.Title)
				}
				if c.Timeout != "60s" {
					t.Errorf("Timeout = %q", c.Timeout)
				}
				if c.ContextWindow != 4096 {
					t.Errorf("ContextWindow = %d", c.ContextWindow)
				}
			},
		},
		{
			name: "defaults applied",
			yaml: `
api_key: sk-or-test
model: openai/gpt-4o
`,
			check: func(t *testing.T, c Config) {
				t.Helper()
				if c.BaseURL != defaultBaseURL {
					t.Errorf("BaseURL = %q, want %q", c.BaseURL, defaultBaseURL)
				}
				if c.Timeout != defaultTimeout {
					t.Errorf("Timeout = %q, want %q", c.Timeout, defaultTimeout)
				}
			},
		},
		{
			name:    "invalid yaml type",
			yaml:    `42`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var node yaml.Node
			if err := yaml.Unmarshal([]byte(tt.yaml), &node); err != nil {
				if tt.wantErr {
					return
				}
				t.Fatalf("yaml.Unmarshal: %v", err)
			}

			o := &OpenRouter{}
			err := o.Configure(&node)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Configure() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.check != nil {
				tt.check(t, o.config)
			}
		})
	}
}

func TestProvision(t *testing.T) {
	t.Parallel()

	t.Run("valid timeout", func(t *testing.T) {
		t.Parallel()

		o := &OpenRouter{}
		o.config.Timeout = "30s"

		ctx := core.NewAppContext(nil, t.TempDir(), t.TempDir())
		if err := o.Provision(ctx); err != nil {
			t.Fatalf("Provision() error = %v", err)
		}
		if o.client == nil {
			t.Fatal("client is nil after Provision")
		}

		svc, ok := ctx.GetService("provider.openrouter")
		if !ok {
			t.Fatal("service not registered")
		}
		if svc != o {
			t.Error("registered service is not the same instance")
		}
	})

	t.Run("invalid timeout", func(t *testing.T) {
		t.Parallel()

		o := &OpenRouter{}
		o.config.Timeout = "not-a-duration"

		ctx := core.NewAppContext(nil, t.TempDir(), t.TempDir())
		if err := o.Provision(ctx); err == nil {
			t.Fatal("Provision() should fail with invalid timeout")
		}
	})
}

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		apiKey  string
		model   string
		baseURL string
		wantErr bool
	}{
		{name: "valid", apiKey: "sk-or-test", model: "openai/gpt-4o", baseURL: defaultBaseURL},
		{name: "valid http", apiKey: "sk-or-test", model: "openai/gpt-4o", baseURL: "http://localhost:8080"},
		{name: "missing api_key", model: "openai/gpt-4o", baseURL: defaultBaseURL, wantErr: true},
		{name: "missing model", apiKey: "sk-or-test", baseURL: defaultBaseURL, wantErr: true},
		{name: "both missing", baseURL: defaultBaseURL, wantErr: true},
		{name: "invalid scheme", apiKey: "sk-or-test", model: "m", baseURL: "ftp://example.com", wantErr: true},
		{name: "no host", apiKey: "sk-or-test", model: "m", baseURL: "https://", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			o := &OpenRouter{config: Config{APIKey: tt.apiKey, Model: tt.model, BaseURL: tt.baseURL}}
			err := o.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
