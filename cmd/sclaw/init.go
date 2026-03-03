// Package main provides the sclaw CLI entry point.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/flemzord/sclaw/pkg/app"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// providerPreset holds the default values for a known LLM provider.
type providerPreset struct {
	Name          string
	BaseURL       string
	APIKeyEnv     string
	Model         string
	ContextWindow int
}

// initResult collects all wizard answers.
type initResult struct {
	// Channel
	TelegramToken string
	TelegramMode  string
	AllowedUsers  string // comma-separated
	AllowedGroups string // comma-separated

	// Provider
	PresetName    string
	BaseURL       string
	APIKeyEnv     string
	Model         string
	ContextWindow string // stored as string for huh Input, parsed to int in generateYAML

	// Agent & Workspace
	DataDir      string
	Workspace    string
	EnableMemory bool
	CreateSoulMD bool

	// Tools
	EnableShellTool     bool
	EnableFileReadTool  bool
	EnableFileWriteTool bool
}

func providerPresets() []providerPreset {
	return []providerPreset{
		{
			Name:          "OpenAI",
			BaseURL:       "https://api.openai.com/v1",
			APIKeyEnv:     "OPENAI_API_KEY",
			Model:         "gpt-4o",
			ContextWindow: 128000,
		},
		{
			Name:          "Anthropic",
			BaseURL:       "https://api.anthropic.com/v1",
			APIKeyEnv:     "ANTHROPIC_API_KEY",
			Model:         "claude-sonnet-4-20250514",
			ContextWindow: 200000,
		},
		{
			Name:          "Ollama",
			BaseURL:       "http://localhost:11434/v1",
			APIKeyEnv:     "",
			Model:         "llama3",
			ContextWindow: 8192,
		},
		{
			Name:          "Custom",
			BaseURL:       "",
			APIKeyEnv:     "",
			Model:         "",
			ContextWindow: 4096,
		},
	}
}

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Generate a configuration file via interactive wizard",
		RunE:  runInit,
	}
}

func runInit(_ *cobra.Command, _ []string) error {
	if !isTerminal() {
		return fmt.Errorf("sclaw init requires an interactive terminal (stdin must be a TTY)")
	}

	cfgPath := defaultConfigPath()

	// Check if config already exists.
	if _, err := os.Stat(cfgPath); err == nil {
		var overwrite bool
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Config file already exists at %s. Overwrite?", cfgPath)).
					Value(&overwrite),
			),
		)
		if err := form.Run(); err != nil {
			return err
		}
		if !overwrite {
			fmt.Println("Aborted.")
			return nil
		}
	}

	result := &initResult{
		TelegramMode:        "polling",
		DataDir:             app.DefaultDataDir(),
		Workspace:           app.DefaultWorkspace(),
		EnableMemory:        true,
		CreateSoulMD:        true,
		EnableShellTool:     true,
		EnableFileReadTool:  true,
		EnableFileWriteTool: true,
	}

	// Form 1: Channel + Preset selection.
	form1 := huh.NewForm(buildChannelGroup(result), buildPresetGroup(result))
	if err := form1.Run(); err != nil {
		return err
	}

	applyPreset(result)

	// Form 2: Provider details (pre-filled) + Agent/Workspace + Tools.
	form2 := huh.NewForm(buildProviderGroup(result), buildAgentGroup(result), buildToolGroup(result))
	if err := form2.Run(); err != nil {
		return err
	}

	// Generate and display YAML.
	data := generateYAML(result)
	fmt.Println("\n--- Generated configuration ---")
	fmt.Println(string(data))
	fmt.Println("-------------------------------")

	// Confirm write.
	var confirm bool
	confirmForm := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Write configuration to %s?", cfgPath)).
				Value(&confirm),
		),
	)
	if err := confirmForm.Run(); err != nil {
		return err
	}
	if !confirm {
		fmt.Println("Aborted.")
		return nil
	}

	if err := writeConfig(cfgPath, data); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	fmt.Printf("Configuration written to %s\n", cfgPath)

	// Write SOUL.md template if requested.
	if result.CreateSoulMD {
		if err := writeSoulTemplate(result.Workspace); err != nil {
			return fmt.Errorf("failed to write SOUL.md: %w", err)
		}
		soulPath := filepath.Join(result.Workspace, "SOUL.md")
		fmt.Printf("SOUL.md template written to %s\n", soulPath)
	}

	fmt.Println("\nNext steps:")
	fmt.Println("  1. Set environment variables (e.g. export TELEGRAM_BOT_TOKEN=...)")
	if result.APIKeyEnv != "" {
		fmt.Printf("  2. Set %s with your API key\n", result.APIKeyEnv)
	}
	fmt.Println("  3. Run: sclaw start")

	return nil
}

func buildChannelGroup(r *initResult) *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Title("Telegram bot token").
			Description("Format: 123456:ABC-DEF (use ${TELEGRAM_BOT_TOKEN} for env var)").
			Placeholder("${TELEGRAM_BOT_TOKEN}").
			Value(&r.TelegramToken),
		huh.NewSelect[string]().
			Title("Telegram mode").
			Options(
				huh.NewOption("Polling (recommended)", "polling"),
				huh.NewOption("Webhook", "webhook"),
			).
			Value(&r.TelegramMode),
		huh.NewInput().
			Title("Allowed user IDs (comma-separated, leave empty for all)").
			Placeholder("123456789, 987654321").
			Value(&r.AllowedUsers),
	).Title("Channel — Telegram")
}

func buildPresetGroup(r *initResult) *huh.Group {
	presets := providerPresets()
	opts := make([]huh.Option[string], 0, len(presets))
	for _, p := range presets {
		opts = append(opts, huh.NewOption(p.Name, p.Name))
	}
	return huh.NewGroup(
		huh.NewSelect[string]().
			Title("LLM Provider preset").
			Options(opts...).
			Value(&r.PresetName),
	).Title("Provider — LLM")
}

func buildProviderGroup(r *initResult) *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Title("Base URL").
			Value(&r.BaseURL),
		huh.NewInput().
			Title("API key environment variable name").
			Description("Leave empty if no key needed (e.g. local Ollama)").
			Value(&r.APIKeyEnv),
		huh.NewInput().
			Title("Model name").
			Value(&r.Model),
		huh.NewInput().
			Title("Context window (tokens)").
			Value(&r.ContextWindow),
	).Title("Provider — Details")
}

func buildAgentGroup(r *initResult) *huh.Group {
	return huh.NewGroup(
		huh.NewInput().
			Title("Data directory").
			Value(&r.DataDir),
		huh.NewInput().
			Title("Workspace directory").
			Value(&r.Workspace),
		huh.NewConfirm().
			Title("Enable SQLite memory?").
			Value(&r.EnableMemory),
		huh.NewConfirm().
			Title("Create a SOUL.md template in workspace?").
			Value(&r.CreateSoulMD),
	).Title("Agent & Workspace")
}

func buildToolGroup(r *initResult) *huh.Group {
	return huh.NewGroup(
		huh.NewConfirm().
			Title("Enable shell execution tool (exec)?").
			Value(&r.EnableShellTool),
		huh.NewConfirm().
			Title("Enable file read tool (read_file)?").
			Value(&r.EnableFileReadTool),
		huh.NewConfirm().
			Title("Enable file write tool (write_file)?").
			Value(&r.EnableFileWriteTool),
	).Title("Tool Modules")
}

func applyPreset(r *initResult) {
	for _, p := range providerPresets() {
		if p.Name == r.PresetName {
			r.BaseURL = p.BaseURL
			r.APIKeyEnv = p.APIKeyEnv
			r.Model = p.Model
			r.ContextWindow = strconv.Itoa(p.ContextWindow)
			return
		}
	}
}

// --- YAML generation ---

// yamlConfig mirrors the runtime config structure for serialization only.
type yamlConfig struct {
	Version string                 `yaml:"version"`
	Modules map[string]interface{} `yaml:"modules"`
	Agents  map[string]interface{} `yaml:"agents,omitempty"`
}

type yamlTelegramModule struct {
	Token       string   `yaml:"token"`
	Mode        string   `yaml:"mode"`
	AllowUsers  []string `yaml:"allow_users,omitempty"`
	AllowGroups []string `yaml:"allow_groups,omitempty"`
}

type yamlProviderModule struct {
	BaseURL       string `yaml:"base_url"`
	APIKey        string `yaml:"api_key,omitempty"`
	APIKeyEnv     string `yaml:"api_key_env,omitempty"`
	Model         string `yaml:"model"`
	ContextWindow int    `yaml:"context_window"`
}

type yamlAgentConfig struct {
	Provider string      `yaml:"provider"`
	Memory   yamlMemory  `yaml:"memory"`
	Routing  yamlRouting `yaml:"routing"`
}

type yamlMemory struct {
	Enabled bool `yaml:"enabled"`
}

type yamlRouting struct {
	Channels []string `yaml:"channels"`
	Default  bool     `yaml:"default"`
}

func generateYAML(r *initResult) []byte {
	modules := make(map[string]interface{})

	// Telegram channel.
	tg := yamlTelegramModule{
		Token: r.TelegramToken,
		Mode:  r.TelegramMode,
	}
	if users := parseCSV(r.AllowedUsers); len(users) > 0 {
		tg.AllowUsers = users
	}
	if groups := parseCSV(r.AllowedGroups); len(groups) > 0 {
		tg.AllowGroups = groups
	}
	modules["channel.telegram"] = tg

	// Provider.
	ctxWin, _ := strconv.Atoi(r.ContextWindow)
	if ctxWin <= 0 {
		ctxWin = 4096
	}
	prov := yamlProviderModule{
		BaseURL:       r.BaseURL,
		Model:         r.Model,
		ContextWindow: ctxWin,
	}
	if r.APIKeyEnv != "" {
		prov.APIKeyEnv = r.APIKeyEnv
	}
	modules["provider.openai_compatible"] = prov

	// Memory.
	if r.EnableMemory {
		modules["memory.sqlite"] = map[string]interface{}{}
	}

	// Tool modules.
	if r.EnableShellTool {
		modules["tool.shell"] = map[string]interface{}{}
	}
	if r.EnableFileReadTool {
		modules["tool.file_read"] = map[string]interface{}{}
	}
	if r.EnableFileWriteTool {
		modules["tool.file_write"] = map[string]interface{}{}
	}

	cfg := yamlConfig{
		Version: "1",
		Modules: modules,
		Agents: map[string]interface{}{
			"main": yamlAgentConfig{
				Provider: "provider.openai_compatible",
				Memory:   yamlMemory{Enabled: r.EnableMemory},
				Routing: yamlRouting{
					Channels: []string{"telegram"},
					Default:  true,
				},
			},
		},
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	_ = enc.Encode(cfg)
	_ = enc.Close()
	return buf.Bytes()
}

func defaultConfigPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "sclaw.yaml"
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "sclaw", "sclaw.yaml")
}

func writeConfig(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func writeSoulTemplate(workspace string) error {
	soulPath := filepath.Join(workspace, "SOUL.md")
	if _, err := os.Stat(soulPath); err == nil {
		// SOUL.md already exists, don't overwrite.
		return nil
	}
	content := `# SOUL.md — Agent Personality

## Identity
You are a helpful personal AI assistant.

## Tone
- Friendly and concise
- Use clear, direct language
- Ask clarifying questions when the request is ambiguous

## Guidelines
- Prioritize accuracy over speed
- Cite sources when possible
- Respect user privacy — never store or repeat sensitive information
`
	return os.WriteFile(soulPath, []byte(content), 0o644)
}

// parseCSV splits a comma-separated string into trimmed, non-empty values.
func parseCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
