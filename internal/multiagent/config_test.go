package multiagent

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// mustYAMLNodes converts raw YAML strings into yaml.Node values for testing.
func mustYAMLNodes(t *testing.T, raw map[string]string) map[string]yaml.Node {
	t.Helper()
	nodes := make(map[string]yaml.Node, len(raw))
	for k, v := range raw {
		var node yaml.Node
		if err := yaml.Unmarshal([]byte(v), &node); err != nil {
			t.Fatalf("mustYAMLNodes(%q): %v", k, err)
		}
		if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
			nodes[k] = *node.Content[0]
		} else {
			nodes[k] = node
		}
	}
	return nodes
}

func TestResolveDefaults_AutoDataDir(t *testing.T) {
	t.Parallel()

	agents := map[string]AgentConfig{
		"assistant": {},
		"researcher": {
			DataDir: "/custom/path",
		},
	}

	ResolveDefaults(agents, "/data")

	if got := agents["assistant"].DataDir; got != filepath.Join("/data", "agents", "assistant") {
		t.Errorf("assistant.DataDir = %q, want %q", got, filepath.Join("/data", "agents", "assistant"))
	}
	if got := agents["researcher"].DataDir; got != "/custom/path" {
		t.Errorf("researcher.DataDir = %q, want %q (custom path should be preserved)", got, "/custom/path")
	}
}

func TestEnsureDirectories_CreatesDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	agentDir := filepath.Join(dir, "agents", "bot")

	agents := map[string]AgentConfig{
		"bot": {DataDir: agentDir},
	}

	if err := EnsureDirectories(agents); err != nil {
		t.Fatalf("EnsureDirectories() error = %v", err)
	}

	info, err := os.Stat(agentDir)
	if err != nil {
		t.Fatalf("agent data dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("agent data dir is not a directory")
	}
}

func TestEnsureDirectories_SkipsEmptyDataDir(t *testing.T) {
	t.Parallel()

	agents := map[string]AgentConfig{
		"empty": {DataDir: ""},
	}

	if err := EnsureDirectories(agents); err != nil {
		t.Fatalf("EnsureDirectories() error = %v", err)
	}
}

func TestMemoryConfig_IsEnabled_DefaultTrue(t *testing.T) {
	t.Parallel()

	mc := MemoryConfig{}
	if !mc.IsEnabled() {
		t.Error("IsEnabled() = false, want true (nil Enabled should default to true)")
	}
}

func TestMemoryConfig_IsEnabled_ExplicitTrue(t *testing.T) {
	t.Parallel()

	v := true
	mc := MemoryConfig{Enabled: &v}
	if !mc.IsEnabled() {
		t.Error("IsEnabled() = false, want true")
	}
}

func TestMemoryConfig_IsEnabled_ExplicitFalse(t *testing.T) {
	t.Parallel()

	v := false
	mc := MemoryConfig{Enabled: &v}
	if mc.IsEnabled() {
		t.Error("IsEnabled() = true, want false")
	}
}

func TestParseAgents_WithMemoryAndDataDir(t *testing.T) {
	t.Parallel()

	nodes := mustYAMLNodes(t, map[string]string{
		"bot": `
data_dir: /custom/bot
provider: provider.openai
memory:
  enabled: false
routing:
  default: true
`,
	})

	agents, _, err := ParseAgents(nodes)
	if err != nil {
		t.Fatalf("ParseAgents() error = %v", err)
	}

	bot := agents["bot"]
	if bot.DataDir != "/custom/bot" {
		t.Errorf("DataDir = %q, want %q", bot.DataDir, "/custom/bot")
	}
	if bot.Memory.IsEnabled() {
		t.Error("Memory.IsEnabled() = true, want false")
	}
}
