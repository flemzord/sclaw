package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateYAML_OpenAIPreset(t *testing.T) {
	r := &initResult{
		TelegramToken: "${TELEGRAM_BOT_TOKEN}",
		TelegramMode:  "polling",
		AllowedUsers:  "123456",
		PresetName:    "OpenAI",
		BaseURL:       "https://api.openai.com/v1",
		APIKeyEnv:     "OPENAI_API_KEY",
		Model:         "gpt-4o",
		ContextWindow: "128000",
		EnableMemory:  true,
	}

	out := string(generateYAML(r))

	assertContains(t, out, `version: "1"`)
	assertContains(t, out, `base_url: https://api.openai.com/v1`)
	assertContains(t, out, `api_key_env: OPENAI_API_KEY`)
	assertContains(t, out, `model: gpt-4o`)
	assertContains(t, out, `context_window: 128000`)
	assertContains(t, out, `token: ${TELEGRAM_BOT_TOKEN}`)
	assertContains(t, out, `memory.sqlite`)
	assertContains(t, out, `"123456"`)
}

func TestGenerateYAML_OllamaNoAPIKey(t *testing.T) {
	r := &initResult{
		TelegramToken: "${TELEGRAM_BOT_TOKEN}",
		TelegramMode:  "polling",
		PresetName:    "Ollama",
		BaseURL:       "http://localhost:11434/v1",
		APIKeyEnv:     "",
		Model:         "llama3",
		ContextWindow: "8192",
		EnableMemory:  true,
	}

	out := string(generateYAML(r))

	assertContains(t, out, `base_url: http://localhost:11434/v1`)
	assertContains(t, out, `model: llama3`)
	assertContains(t, out, `context_window: 8192`)
	assertNotContains(t, out, `api_key_env`)
	assertNotContains(t, out, `api_key:`)
}

func TestGenerateYAML_WithMemory(t *testing.T) {
	r := &initResult{
		TelegramToken: "tok",
		TelegramMode:  "polling",
		BaseURL:       "http://localhost:11434/v1",
		Model:         "llama3",
		ContextWindow: "8192",
		EnableMemory:  true,
	}

	out := string(generateYAML(r))
	assertContains(t, out, `memory.sqlite`)
	assertContains(t, out, `enabled: true`)
}

func TestGenerateYAML_WithoutMemory(t *testing.T) {
	r := &initResult{
		TelegramToken: "tok",
		TelegramMode:  "polling",
		BaseURL:       "http://localhost:11434/v1",
		Model:         "llama3",
		ContextWindow: "8192",
		EnableMemory:  false,
	}

	out := string(generateYAML(r))
	assertNotContains(t, out, `memory.sqlite`)
	assertContains(t, out, `enabled: false`)
}

func TestGenerateYAML_InvalidContextWindow(t *testing.T) {
	r := &initResult{
		TelegramToken: "tok",
		TelegramMode:  "polling",
		BaseURL:       "http://localhost:11434/v1",
		Model:         "llama3",
		ContextWindow: "not-a-number",
		EnableMemory:  false,
	}

	out := string(generateYAML(r))
	// Should fall back to 4096.
	assertContains(t, out, `context_window: 4096`)
}

func TestDefaultConfigPath(t *testing.T) {
	t.Run("with XDG_CONFIG_HOME", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test")
		got := defaultConfigPath()
		want := "/tmp/xdg-test/sclaw/sclaw.yaml"
		if got != want {
			t.Errorf("defaultConfigPath() = %q, want %q", got, want)
		}
	})

	t.Run("without XDG_CONFIG_HOME", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		got := defaultConfigPath()
		home, _ := os.UserHomeDir()
		want := filepath.Join(home, ".config", "sclaw", "sclaw.yaml")
		if got != want {
			t.Errorf("defaultConfigPath() = %q, want %q", got, want)
		}
	})
}

func TestParseCSV(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty string", "", nil},
		{"spaces only", "   ", nil},
		{"single value", "123", []string{"123"}},
		{"two values", "123, 456", []string{"123", "456"}},
		{"extra commas", ",123,,456,", []string{"123", "456"}},
		{"spaces around", " a , b , c ", []string{"a", "b", "c"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCSV(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("parseCSV(%q) = %v, want %v", tt.input, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseCSV(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestWriteConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "sclaw.yaml")
	data := []byte("version: \"1\"\n")

	if err := writeConfig(path, data); err != nil {
		t.Fatalf("writeConfig() error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("writeConfig wrote %q, want %q", got, data)
	}
}

func TestWriteSoulTemplate(t *testing.T) {
	t.Run("creates file", func(t *testing.T) {
		dir := t.TempDir()
		if err := writeSoulTemplate(dir); err != nil {
			t.Fatalf("writeSoulTemplate() error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(dir, "SOUL.md"))
		if err != nil {
			t.Fatalf("ReadFile() error: %v", err)
		}
		assertContains(t, string(got), "Agent Personality")
	})

	t.Run("does not overwrite existing", func(t *testing.T) {
		dir := t.TempDir()
		existing := filepath.Join(dir, "SOUL.md")
		if err := os.WriteFile(existing, []byte("custom"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := writeSoulTemplate(dir); err != nil {
			t.Fatalf("writeSoulTemplate() error: %v", err)
		}
		got, _ := os.ReadFile(existing)
		if string(got) != "custom" {
			t.Errorf("writeSoulTemplate overwrote existing file")
		}
	})
}

func TestApplyPreset(t *testing.T) {
	r := &initResult{PresetName: "Anthropic"}
	applyPreset(r)

	if r.BaseURL != "https://api.anthropic.com/v1" {
		t.Errorf("BaseURL = %q, want Anthropic URL", r.BaseURL)
	}
	if r.APIKeyEnv != "ANTHROPIC_API_KEY" {
		t.Errorf("APIKeyEnv = %q, want ANTHROPIC_API_KEY", r.APIKeyEnv)
	}
	if r.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want claude-sonnet-4-20250514", r.Model)
	}
	if r.ContextWindow != "200000" {
		t.Errorf("ContextWindow = %q, want 200000", r.ContextWindow)
	}
}

// --- helpers ---

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected output to contain %q, got:\n%s", needle, haystack)
	}
}

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("expected output NOT to contain %q, got:\n%s", needle, haystack)
	}
}
