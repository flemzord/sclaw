package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_NoFile(t *testing.T) {
	cfg, err := LoadConfig(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatal("expected nil config for missing file")
	}
}

func TestLoadConfig_ValidStdio(t *testing.T) {
	dir := t.TempDir()
	data := `{
		"mcpServers": {
			"fs": {
				"command": "npx",
				"args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(cfg.Servers))
	}

	srv := cfg.Servers["fs"]
	if !srv.IsStdio() {
		t.Fatal("expected stdio transport")
	}
	if srv.IsHTTP() {
		t.Fatal("expected non-HTTP transport")
	}
	if srv.Command != "npx" {
		t.Fatalf("expected command 'npx', got %q", srv.Command)
	}
	if len(srv.Args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(srv.Args))
	}
}

func TestLoadConfig_ValidHTTP(t *testing.T) {
	dir := t.TempDir()
	data := `{
		"mcpServers": {
			"posthog": {
				"url": "https://mcp.posthog.com/mcp",
				"headers": {
					"Authorization": "Bearer sk-test"
				}
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	srv := cfg.Servers["posthog"]
	if !srv.IsHTTP() {
		t.Fatal("expected HTTP transport")
	}
	if srv.URL != "https://mcp.posthog.com/mcp" {
		t.Fatalf("unexpected URL: %q", srv.URL)
	}
	if srv.Headers["Authorization"] != "Bearer sk-test" {
		t.Fatalf("unexpected header: %q", srv.Headers["Authorization"])
	}
}

func TestLoadConfig_InvalidBothTransports(t *testing.T) {
	dir := t.TempDir()
	data := `{
		"mcpServers": {
			"bad": {
				"command": "npx",
				"url": "https://example.com"
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(dir)
	if err == nil {
		t.Fatal("expected error for server with both command and url")
	}
}

func TestLoadConfig_InvalidNeitherTransport(t *testing.T) {
	dir := t.TempDir()
	data := `{
		"mcpServers": {
			"bad": {
				"args": ["-v"]
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(dir)
	if err == nil {
		t.Fatal("expected error for server with neither command nor url")
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte("{not json}"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(dir)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadConfig_EnvExpansion(t *testing.T) {
	dir := t.TempDir()
	data := `{
		"mcpServers": {
			"api": {
				"url": "https://example.com/mcp",
				"headers": {
					"Authorization": "Bearer ${TEST_MCP_TOKEN}"
				}
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TEST_MCP_TOKEN", "my-secret-token")

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := cfg.Servers["api"].Headers["Authorization"]
	if got != "Bearer my-secret-token" {
		t.Fatalf("expected expanded token, got %q", got)
	}
}

func TestLoadConfig_EnvExpansionWithDefault(t *testing.T) {
	dir := t.TempDir()
	data := `{
		"mcpServers": {
			"api": {
				"url": "${TEST_MCP_URL:-https://fallback.example.com/mcp}"
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	// Ensure the env var is NOT set so the default is used.
	t.Setenv("TEST_MCP_URL", "")
	os.Unsetenv("TEST_MCP_URL")

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := cfg.Servers["api"].URL
	if got != "https://fallback.example.com/mcp" {
		t.Fatalf("expected default URL, got %q", got)
	}
}

func TestExpandEnvVars(t *testing.T) {
	t.Setenv("EXPAND_TEST_VAR", "hello")

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", "${EXPAND_TEST_VAR}", "hello"},
		{"with_text", "prefix-${EXPAND_TEST_VAR}-suffix", "prefix-hello-suffix"},
		{"unset_no_default", "${EXPAND_UNSET_VAR_XYZ}", "${EXPAND_UNSET_VAR_XYZ}"},
		{"unset_with_default", "${EXPAND_UNSET_VAR_XYZ:-fallback}", "fallback"},
		{"no_vars", "plain text", "plain text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandEnvVars(tt.input)
			if got != tt.want {
				t.Errorf("expandEnvVars(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
