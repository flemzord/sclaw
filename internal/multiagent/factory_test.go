package multiagent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/flemzord/sclaw/internal/memory"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/provider/providertest"
	"github.com/flemzord/sclaw/internal/router"
	"github.com/flemzord/sclaw/internal/tool"
	"github.com/flemzord/sclaw/internal/tool/tooltest"
	"github.com/flemzord/sclaw/internal/workspace"
	"github.com/flemzord/sclaw/pkg/message"
)

// newStubProvider returns a MockProvider with minimal stubs so it satisfies
// the provider.Provider interface without panicking on unused methods.
func newStubProvider() *providertest.MockProvider {
	return &providertest.MockProvider{
		CompleteFunc: func(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
			return provider.CompletionResponse{Content: "ok"}, nil
		},
		StreamFunc: func(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
			ch := make(chan provider.StreamChunk)
			close(ch)
			return ch, nil
		},
		ContextWindowSizeFunc: func() int { return 4096 },
		ModelNameFunc:         func() string { return "stub" },
		HealthCheckFunc:       func(_ context.Context) error { return nil },
	}
}

// newGlobalTools creates a tool.Registry pre-populated with the given tool names.
func newGlobalTools(t *testing.T, names ...string) *tool.Registry {
	t.Helper()
	reg := tool.NewRegistry()
	for _, n := range names {
		if err := reg.Register(tooltest.SimpleTool(n, tool.ApprovalAllow)); err != nil {
			t.Fatalf("registering tool %q: %v", n, err)
		}
	}
	return reg
}

func TestMultiAgentFactory_ForSession_NewSession(t *testing.T) {
	t.Parallel()

	agents := map[string]AgentConfig{
		"support": {
			Workspace: "/ws/support",
			Routing:   RoutingConfig{Users: []string{"user-42"}},
		},
		"fallback": {
			Workspace: "/ws/fallback",
			Routing:   RoutingConfig{Default: true},
		},
	}
	reg, err := NewRegistry(agents, []string{"fallback", "support"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry:        reg,
		DefaultProvider: newStubProvider(),
		GlobalTools:     newGlobalTools(t, "search"),
	})

	session := &router.Session{ID: "sess-1"}
	msg := message.InboundMessage{
		Channel: "telegram",
		Sender:  message.Sender{ID: "user-42"},
		Chat:    message.Chat{ID: "chat-1"},
	}

	loop, err := factory.ForSession(session, msg)
	if err != nil {
		t.Fatalf("ForSession() error = %v", err)
	}
	if loop == nil {
		t.Fatal("ForSession() returned nil loop")
	}
	if session.AgentID != "support" {
		t.Errorf("session.AgentID = %q, want %q", session.AgentID, "support")
	}
}

func TestMultiAgentFactory_ForSession_ExistingSession(t *testing.T) {
	t.Parallel()

	agents := map[string]AgentConfig{
		"bot-a": {
			Workspace: "/ws/a",
			Routing:   RoutingConfig{Default: true},
		},
		"bot-b": {
			Workspace: "/ws/b",
			Routing:   RoutingConfig{Users: []string{"user-99"}},
		},
	}
	reg, err := NewRegistry(agents, []string{"bot-a", "bot-b"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry:        reg,
		DefaultProvider: newStubProvider(),
		GlobalTools:     newGlobalTools(t, "calc"),
	})

	// Session already has an AgentID — it must be reused, not re-resolved.
	session := &router.Session{ID: "sess-2", AgentID: "bot-a"}
	msg := message.InboundMessage{
		Channel: "slack",
		Sender:  message.Sender{ID: "user-99"}, // would resolve to bot-b
		Chat:    message.Chat{ID: "chat-2"},
	}

	loop, err := factory.ForSession(session, msg)
	if err != nil {
		t.Fatalf("ForSession() error = %v", err)
	}
	if loop == nil {
		t.Fatal("ForSession() returned nil loop")
	}
	if session.AgentID != "bot-a" {
		t.Errorf("session.AgentID = %q, want %q (should not be overwritten)", session.AgentID, "bot-a")
	}
}

func TestMultiAgentFactory_ForSession_NoMatch(t *testing.T) {
	t.Parallel()

	agents := map[string]AgentConfig{
		"niche": {
			Routing: RoutingConfig{Users: []string{"special"}},
		},
	}
	reg, err := NewRegistry(agents, []string{"niche"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry:        reg,
		DefaultProvider: newStubProvider(),
		GlobalTools:     tool.NewRegistry(),
	})

	session := &router.Session{ID: "sess-3"}
	msg := message.InboundMessage{
		Channel: "discord",
		Sender:  message.Sender{ID: "nobody"},
		Chat:    message.Chat{ID: "nowhere"},
	}

	_, err = factory.ForSession(session, msg)
	if err == nil {
		t.Fatal("ForSession() expected error, got nil")
	}
	if !errors.Is(err, ErrNoMatchingAgent) {
		t.Errorf("ForSession() error = %v, want %v", err, ErrNoMatchingAgent)
	}
}

func TestMultiAgentFactory_ForSession_AgentNotFound(t *testing.T) {
	t.Parallel()

	agents := map[string]AgentConfig{
		"only-one": {
			Routing: RoutingConfig{Default: true},
		},
	}
	reg, err := NewRegistry(agents, []string{"only-one"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry:        reg,
		DefaultProvider: newStubProvider(),
		GlobalTools:     tool.NewRegistry(),
	})

	// Session references an agent that does not exist in the registry.
	session := &router.Session{ID: "sess-4", AgentID: "ghost"}
	msg := message.InboundMessage{
		Channel: "telegram",
		Sender:  message.Sender{ID: "someone"},
		Chat:    message.Chat{ID: "chat-4"},
	}

	_, err = factory.ForSession(session, msg)
	if err == nil {
		t.Fatal("ForSession() expected error, got nil")
	}
	if !errors.Is(err, ErrAgentNotFound) {
		t.Errorf("ForSession() error = %v, want %v", err, ErrAgentNotFound)
	}
}

func TestMultiAgentFactory_ToolFiltering(t *testing.T) {
	t.Parallel()

	agents := map[string]AgentConfig{
		"limited": {
			Workspace: "/ws/limited",
			Tools:     []string{"search", "calc"},
			Routing:   RoutingConfig{Default: true},
		},
	}
	reg, err := NewRegistry(agents, []string{"limited"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	globalTools := newGlobalTools(t, "search", "calc", "exec", "deploy")

	factory := NewFactory(FactoryConfig{
		Registry:        reg,
		DefaultProvider: newStubProvider(),
		GlobalTools:     globalTools,
	})

	session := &router.Session{ID: "sess-5"}
	msg := message.InboundMessage{
		Channel: "slack",
		Sender:  message.Sender{ID: "dev"},
		Chat:    message.Chat{ID: "chat-5"},
	}

	loop, err := factory.ForSession(session, msg)
	if err != nil {
		t.Fatalf("ForSession() error = %v", err)
	}
	if loop == nil {
		t.Fatal("ForSession() returned nil loop")
	}

	// Verify filtering by inspecting the filtered registry built internally.
	// We call buildToolRegistry directly to check its output.
	agentCfg, _ := reg.AgentConfig("limited")
	filtered := factory.buildToolRegistry(agentCfg)

	names := filtered.Names()
	if len(names) != 2 {
		t.Fatalf("filtered tools count = %d, want 2; got %v", len(names), names)
	}
	want := map[string]bool{"search": true, "calc": true}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected tool %q in filtered registry", n)
		}
	}

	// Tools not in the allowlist must not be present.
	for _, excluded := range []string{"exec", "deploy"} {
		if _, err := filtered.Get(excluded); err == nil {
			t.Errorf("tool %q should not be in filtered registry", excluded)
		}
	}
}

func TestMultiAgentFactory_ToolFiltering_EmptyReturnsGlobal(t *testing.T) {
	t.Parallel()

	agents := map[string]AgentConfig{
		"full": {
			Workspace: "/ws/full",
			Routing:   RoutingConfig{Default: true},
			// No Tools filter — should get the global registry.
		},
	}
	reg, err := NewRegistry(agents, []string{"full"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	globalTools := newGlobalTools(t, "alpha", "beta")

	factory := NewFactory(FactoryConfig{
		Registry:        reg,
		DefaultProvider: newStubProvider(),
		GlobalTools:     globalTools,
	})

	agentCfg, _ := reg.AgentConfig("full")
	result := factory.buildToolRegistry(agentCfg)

	if result != globalTools {
		t.Error("buildToolRegistry() should return the global registry when Tools is empty")
	}
}

func TestMultiAgentFactory_BuildLoopConfig(t *testing.T) {
	t.Parallel()

	factory := NewFactory(FactoryConfig{})

	cfg := AgentConfig{
		Loop: LoopOverrides{
			MaxIterations: 20,
			TokenBudget:   50000,
			Timeout:       "2m30s",
			LoopThreshold: 5,
		},
	}

	lc := factory.buildLoopConfig(cfg)

	if lc.MaxIterations != 20 {
		t.Errorf("MaxIterations = %d, want 20", lc.MaxIterations)
	}
	if lc.TokenBudget != 50000 {
		t.Errorf("TokenBudget = %d, want 50000", lc.TokenBudget)
	}
	if lc.Timeout.Seconds() != 150 {
		t.Errorf("Timeout = %v, want 2m30s", lc.Timeout)
	}
	if lc.LoopThreshold != 5 {
		t.Errorf("LoopThreshold = %d, want 5", lc.LoopThreshold)
	}
}

func TestMultiAgentFactory_BuildLoopConfig_InvalidTimeout(t *testing.T) {
	t.Parallel()

	factory := NewFactory(FactoryConfig{})

	cfg := AgentConfig{
		Loop: LoopOverrides{
			Timeout: "not-a-duration",
		},
	}

	lc := factory.buildLoopConfig(cfg)

	if lc.Timeout != 0 {
		t.Errorf("Timeout = %v, want 0 for invalid duration string", lc.Timeout)
	}
}

func TestFactory_ResolveHistory_WithOverride(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	agents := map[string]AgentConfig{
		"bot": {
			DataDir: filepath.Join(tmpDir, "agents", "bot"),
			Routing: RoutingConfig{Default: true},
		},
	}
	ResolveDefaults(agents, tmpDir)
	if err := EnsureDirectories(agents); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}
	reg, err := NewRegistry(agents, []string{"bot"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	// Use an in-memory store as the module-provided override.
	override := memory.NewInMemoryHistoryStore()

	factory := NewFactory(FactoryConfig{
		Registry:     reg,
		Logger:       slog.Default(),
		HistoryStore: override,
	})
	defer func() { _ = factory.Close() }()

	store := factory.ResolveHistory("bot")
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	if store != override {
		t.Error("expected ResolveHistory to return the injected HistoryStore, not a new SQLite store")
	}

	// Second call should return the same cached instance.
	store2 := factory.ResolveHistory("bot")
	if store2 != override {
		t.Error("expected cached store to be the injected override")
	}
}

func TestFactory_ResolveHistory_Cached(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	agents := map[string]AgentConfig{
		"bot": {
			DataDir: filepath.Join(tmpDir, "agents", "bot"),
			Routing: RoutingConfig{Default: true},
		},
	}
	ResolveDefaults(agents, tmpDir)
	if err := EnsureDirectories(agents); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}
	reg, err := NewRegistry(agents, []string{"bot"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry: reg,
		Logger:   slog.Default(),
	})
	defer func() { _ = factory.Close() }()

	store1 := factory.ResolveHistory("bot")
	if store1 == nil {
		t.Fatal("expected non-nil store for agent with memory enabled")
	}

	store2 := factory.ResolveHistory("bot")
	if store1 != store2 {
		t.Error("expected same store instance on second call (cache hit)")
	}
}

func TestFactory_ResolveHistory_Disabled(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	disabled := false
	agents := map[string]AgentConfig{
		"silent": {
			DataDir: filepath.Join(tmpDir, "agents", "silent"),
			Memory:  MemoryConfig{Enabled: &disabled},
			Routing: RoutingConfig{Default: true},
		},
	}
	if err := EnsureDirectories(agents); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}
	reg, err := NewRegistry(agents, []string{"silent"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry: reg,
		Logger:   slog.Default(),
	})
	defer func() { _ = factory.Close() }()

	store := factory.ResolveHistory("silent")
	if store != nil {
		t.Error("expected nil store for agent with memory disabled")
	}
}

func TestFactory_ResolveHistory_UnknownAgent(t *testing.T) {
	t.Parallel()

	agents := map[string]AgentConfig{
		"known": {
			Routing: RoutingConfig{Default: true},
		},
	}
	reg, err := NewRegistry(agents, []string{"known"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry: reg,
		Logger:   slog.Default(),
	})
	defer func() { _ = factory.Close() }()

	store := factory.ResolveHistory("ghost")
	if store != nil {
		t.Error("expected nil store for unknown agent")
	}
}

func TestFactory_Close(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	agents := map[string]AgentConfig{
		"a": {
			DataDir: filepath.Join(tmpDir, "agents", "a"),
			Routing: RoutingConfig{Default: true},
		},
		"b": {
			DataDir: filepath.Join(tmpDir, "agents", "b"),
			Routing: RoutingConfig{Channels: []string{"slack"}},
		},
	}
	ResolveDefaults(agents, tmpDir)
	if err := EnsureDirectories(agents); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}
	reg, err := NewRegistry(agents, []string{"a", "b"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry: reg,
		Logger:   slog.Default(),
	})

	// Open stores for both agents.
	if s := factory.ResolveHistory("a"); s == nil {
		t.Fatal("expected non-nil store for agent a")
	}
	if s := factory.ResolveHistory("b"); s == nil {
		t.Fatal("expected non-nil store for agent b")
	}

	// Close should not error.
	if err := factory.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// After Close, stores map should be reset.
	factory.mu.RLock()
	n := len(factory.stores)
	factory.mu.RUnlock()
	if n != 0 {
		t.Errorf("stores map has %d entries after Close, want 0", n)
	}
}

func TestFactory_ResolveSoul_LoadsFromFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	agents := map[string]AgentConfig{
		"persona": {
			DataDir: filepath.Join(tmpDir, "agents", "persona"),
			Routing: RoutingConfig{Default: true},
		},
	}
	ResolveDefaults(agents, tmpDir)
	if err := EnsureDirectories(agents); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}
	reg, err := NewRegistry(agents, []string{"persona"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	// Write a SOUL.md file.
	soulPath := filepath.Join(agents["persona"].DataDir, "SOUL.md")
	if err := os.WriteFile(soulPath, []byte("You are a pirate captain."), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry: reg,
		Logger:   slog.Default(),
	})

	prompt, err := factory.ResolveSoul("persona")
	if err != nil {
		t.Fatalf("ResolveSoul: %v", err)
	}
	if prompt != "You are a pirate captain." {
		t.Errorf("prompt = %q, want %q", prompt, "You are a pirate captain.")
	}
}

func TestFactory_ResolveSoul_DefaultWhenMissing(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	agents := map[string]AgentConfig{
		"plain": {
			DataDir: filepath.Join(tmpDir, "agents", "plain"),
			Routing: RoutingConfig{Default: true},
		},
	}
	ResolveDefaults(agents, tmpDir)
	if err := EnsureDirectories(agents); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}
	reg, err := NewRegistry(agents, []string{"plain"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry: reg,
		Logger:   slog.Default(),
	})

	prompt, err := factory.ResolveSoul("plain")
	if err != nil {
		t.Fatalf("ResolveSoul: %v", err)
	}
	if prompt != "You are a helpful assistant." {
		t.Errorf("prompt = %q, want default", prompt)
	}
}

func TestFactory_ResolveSoul_Cached(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	agents := map[string]AgentConfig{
		"cached": {
			DataDir: filepath.Join(tmpDir, "agents", "cached"),
			Routing: RoutingConfig{Default: true},
		},
	}
	ResolveDefaults(agents, tmpDir)
	if err := EnsureDirectories(agents); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}
	reg, err := NewRegistry(agents, []string{"cached"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry: reg,
		Logger:   slog.Default(),
	})

	// First call creates the loader.
	_, err = factory.ResolveSoul("cached")
	if err != nil {
		t.Fatalf("first ResolveSoul: %v", err)
	}

	// Verify the loader is cached.
	factory.mu.RLock()
	_, exists := factory.souls["cached"]
	factory.mu.RUnlock()
	if !exists {
		t.Error("expected soul loader to be cached after first call")
	}

	// Second call should reuse the cached loader.
	_, err = factory.ResolveSoul("cached")
	if err != nil {
		t.Fatalf("second ResolveSoul: %v", err)
	}

	// Verify still only one entry in the cache.
	factory.mu.RLock()
	n := len(factory.souls)
	factory.mu.RUnlock()
	if n != 1 {
		t.Errorf("souls cache has %d entries, want 1", n)
	}
}

func TestFactory_ResolveSoul_UnknownAgent(t *testing.T) {
	t.Parallel()

	agents := map[string]AgentConfig{
		"known": {
			Routing: RoutingConfig{Default: true},
		},
	}
	reg, err := NewRegistry(agents, []string{"known"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry: reg,
		Logger:   slog.Default(),
	})

	prompt, err := factory.ResolveSoul("ghost")
	if err != nil {
		t.Fatalf("ResolveSoul: %v", err)
	}
	if prompt != "You are a helpful assistant." {
		t.Errorf("prompt = %q, want default for unknown agent", prompt)
	}
}

// writeSkillFile creates a skill .md file with trigger "always" in the given directory.
func writeSkillFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	content := "---\nname: " + name + "\ntrigger: always\ntools_required: [search]\n---\nSkill body for " + name + ".\n"
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestFactory_ResolveSkills_MergesGlobalAndPerAgent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "skills")
	agentDataDir := filepath.Join(tmpDir, "agents", "bot")

	writeSkillFile(t, globalDir, "global-skill")
	writeSkillFile(t, filepath.Join(agentDataDir, "skills"), "agent-skill")

	agents := map[string]AgentConfig{
		"bot": {
			DataDir: agentDataDir,
			Routing: RoutingConfig{Default: true},
		},
	}
	reg, err := NewRegistry(agents, []string{"bot"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry:        reg,
		GlobalTools:     newGlobalTools(t, "search"),
		Logger:          slog.Default(),
		GlobalSkillsDir: globalDir,
	})

	result, err := factory.ResolveSkills("bot", "hello")
	if err != nil {
		t.Fatalf("ResolveSkills: %v", err)
	}
	if !strings.Contains(result, "<name>global-skill</name>") {
		t.Errorf("result missing global-skill:\n%s", result)
	}
	if !strings.Contains(result, "<name>agent-skill</name>") {
		t.Errorf("result missing agent-skill:\n%s", result)
	}
}

func TestFactory_ResolveSkills_ExcludeSkillsFiltersGlobal(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "skills")
	agentDataDir := filepath.Join(tmpDir, "agents", "bot")

	writeSkillFile(t, globalDir, "keep-me")
	writeSkillFile(t, globalDir, "drop-me")

	agents := map[string]AgentConfig{
		"bot": {
			DataDir:       agentDataDir,
			ExcludeSkills: []string{"drop-me"},
			Routing:       RoutingConfig{Default: true},
		},
	}
	reg, err := NewRegistry(agents, []string{"bot"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry:        reg,
		GlobalTools:     newGlobalTools(t, "search"),
		Logger:          slog.Default(),
		GlobalSkillsDir: globalDir,
	})

	result, err := factory.ResolveSkills("bot", "hello")
	if err != nil {
		t.Fatalf("ResolveSkills: %v", err)
	}
	if !strings.Contains(result, "<name>keep-me</name>") {
		t.Errorf("result missing keep-me:\n%s", result)
	}
	if strings.Contains(result, "<name>drop-me</name>") {
		t.Errorf("result should not contain drop-me:\n%s", result)
	}
}

func TestFactory_ResolveSkills_PerAgentNotAffectedByExclude(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "skills")
	agentDataDir := filepath.Join(tmpDir, "agents", "bot")

	// Per-agent skill with same name as an excluded skill — should NOT be excluded.
	writeSkillFile(t, filepath.Join(agentDataDir, "skills"), "my-skill")

	agents := map[string]AgentConfig{
		"bot": {
			DataDir:       agentDataDir,
			ExcludeSkills: []string{"my-skill"},
			Routing:       RoutingConfig{Default: true},
		},
	}
	reg, err := NewRegistry(agents, []string{"bot"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry:        reg,
		GlobalTools:     newGlobalTools(t, "search"),
		Logger:          slog.Default(),
		GlobalSkillsDir: globalDir,
	})

	result, err := factory.ResolveSkills("bot", "hello")
	if err != nil {
		t.Fatalf("ResolveSkills: %v", err)
	}
	if !strings.Contains(result, "<name>my-skill</name>") {
		t.Errorf("per-agent skill should not be affected by exclude_skills:\n%s", result)
	}
}

func TestFactory_ResolveSkills_EmptyDirs(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	agentDataDir := filepath.Join(tmpDir, "agents", "bot")
	if err := os.MkdirAll(agentDataDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	agents := map[string]AgentConfig{
		"bot": {
			DataDir: agentDataDir,
			Routing: RoutingConfig{Default: true},
		},
	}
	reg, err := NewRegistry(agents, []string{"bot"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry:        reg,
		GlobalTools:     newGlobalTools(t, "search"),
		Logger:          slog.Default(),
		GlobalSkillsDir: filepath.Join(tmpDir, "skills"),
	})

	result, err := factory.ResolveSkills("bot", "hello")
	if err != nil {
		t.Fatalf("ResolveSkills: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result for empty dirs, got %q", result)
	}
}

func TestFactory_ResolveSkills_BuiltinSkills(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	agentDataDir := filepath.Join(tmpDir, "agents", "bot")
	if err := os.MkdirAll(agentDataDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	builtinFS := fstest.MapFS{
		"builtin-skill/SKILL.md": &fstest.MapFile{
			Data: []byte("---\nname: builtin-skill\ntrigger: always\ntools_required: [search]\n---\nBuiltin body.\n"),
		},
	}

	agents := map[string]AgentConfig{
		"bot": {
			DataDir: agentDataDir,
			Routing: RoutingConfig{Default: true},
		},
	}
	reg, err := NewRegistry(agents, []string{"bot"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry:        reg,
		GlobalTools:     newGlobalTools(t, "search"),
		Logger:          slog.Default(),
		BuiltinSkillsFS: builtinFS,
		GlobalSkillsDir: filepath.Join(tmpDir, "skills"), // empty
	})

	result, err := factory.ResolveSkills("bot", "hello")
	if err != nil {
		t.Fatalf("ResolveSkills: %v", err)
	}
	if !strings.Contains(result, "<name>builtin-skill</name>") {
		t.Errorf("result missing builtin-skill:\n%s", result)
	}
	// Builtin skills should have inline content, not location.
	if !strings.Contains(result, "<content>") {
		t.Errorf("builtin skill should have inline <content>:\n%s", result)
	}
}

func TestFactory_ResolveSkills_GlobalOverridesBuiltin(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	globalDir := filepath.Join(tmpDir, "skills")
	agentDataDir := filepath.Join(tmpDir, "agents", "bot")
	if err := os.MkdirAll(agentDataDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Builtin has "weather" skill.
	builtinFS := fstest.MapFS{
		"weather.md": &fstest.MapFile{
			Data: []byte("---\nname: weather\ntrigger: always\ntools_required: [search]\n---\nBuiltin weather.\n"),
		},
		"coding.md": &fstest.MapFile{
			Data: []byte("---\nname: coding\ntrigger: always\ntools_required: [search]\n---\nBuiltin coding.\n"),
		},
	}

	// Global filesystem also has "weather" — should override builtin.
	writeSkillFile(t, globalDir, "weather")

	agents := map[string]AgentConfig{
		"bot": {
			DataDir: agentDataDir,
			Routing: RoutingConfig{Default: true},
		},
	}
	reg, err := NewRegistry(agents, []string{"bot"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry:        reg,
		GlobalTools:     newGlobalTools(t, "search"),
		Logger:          slog.Default(),
		BuiltinSkillsFS: builtinFS,
		GlobalSkillsDir: globalDir,
	})

	result, err := factory.ResolveSkills("bot", "hello")
	if err != nil {
		t.Fatalf("ResolveSkills: %v", err)
	}

	// Both skills should be present.
	if !strings.Contains(result, "<name>weather</name>") {
		t.Errorf("result missing weather:\n%s", result)
	}
	if !strings.Contains(result, "<name>coding</name>") {
		t.Errorf("result missing coding:\n%s", result)
	}

	// "weather" should have a <location> (from filesystem, not builtin).
	if strings.Contains(result, workspace.BuiltinPathPrefix+"weather.md") {
		t.Errorf("weather should be overridden by global, not builtin:\n%s", result)
	}
	if !strings.Contains(result, "<location>") {
		t.Errorf("overridden weather should have a filesystem <location>:\n%s", result)
	}
}

func TestFactory_ResolveSkills_ExcludeAppliesToBuiltin(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	agentDataDir := filepath.Join(tmpDir, "agents", "bot")
	if err := os.MkdirAll(agentDataDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	builtinFS := fstest.MapFS{
		"keep.md": &fstest.MapFile{
			Data: []byte("---\nname: keep\ntrigger: always\ntools_required: [search]\n---\nKeep body.\n"),
		},
		"drop.md": &fstest.MapFile{
			Data: []byte("---\nname: drop\ntrigger: always\ntools_required: [search]\n---\nDrop body.\n"),
		},
	}

	agents := map[string]AgentConfig{
		"bot": {
			DataDir:       agentDataDir,
			ExcludeSkills: []string{"drop"},
			Routing:       RoutingConfig{Default: true},
		},
	}
	reg, err := NewRegistry(agents, []string{"bot"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry:        reg,
		GlobalTools:     newGlobalTools(t, "search"),
		Logger:          slog.Default(),
		BuiltinSkillsFS: builtinFS,
		GlobalSkillsDir: filepath.Join(tmpDir, "skills"),
	})

	result, err := factory.ResolveSkills("bot", "hello")
	if err != nil {
		t.Fatalf("ResolveSkills: %v", err)
	}
	if !strings.Contains(result, "<name>keep</name>") {
		t.Errorf("result missing keep:\n%s", result)
	}
	if strings.Contains(result, "<name>drop</name>") {
		t.Errorf("result should not contain excluded builtin skill 'drop':\n%s", result)
	}
}

func TestFactory_Reload_SwapsRegistry(t *testing.T) {
	t.Parallel()

	agentsA := map[string]AgentConfig{
		"alpha": {
			Workspace: "/ws/alpha",
			Routing:   RoutingConfig{Default: true},
		},
	}
	regA, err := NewRegistry(agentsA, []string{"alpha"})
	if err != nil {
		t.Fatalf("NewRegistry A: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry:        regA,
		DefaultProvider: newStubProvider(),
		GlobalTools:     newGlobalTools(t, "search"),
		Logger:          slog.Default(),
	})

	// Initially resolves to "alpha".
	session := &router.Session{ID: "s1"}
	msg := message.InboundMessage{
		Channel: "test",
		Sender:  message.Sender{ID: "user"},
		Chat:    message.Chat{ID: "chat"},
	}
	loop, err := factory.ForSession(session, msg)
	if err != nil {
		t.Fatalf("ForSession before reload: %v", err)
	}
	if loop == nil {
		t.Fatal("expected non-nil loop")
	}
	if session.AgentID != "alpha" {
		t.Errorf("AgentID = %q, want %q", session.AgentID, "alpha")
	}

	// Reload with a new registry containing "beta".
	agentsB := map[string]AgentConfig{
		"beta": {
			Workspace: "/ws/beta",
			Routing:   RoutingConfig{Default: true},
		},
	}
	regB, err := NewRegistry(agentsB, []string{"beta"})
	if err != nil {
		t.Fatalf("NewRegistry B: %v", err)
	}
	factory.Reload(regB)

	// New session should resolve to "beta".
	session2 := &router.Session{ID: "s2"}
	loop2, err := factory.ForSession(session2, msg)
	if err != nil {
		t.Fatalf("ForSession after reload: %v", err)
	}
	if loop2 == nil {
		t.Fatal("expected non-nil loop after reload")
	}
	if session2.AgentID != "beta" {
		t.Errorf("AgentID = %q, want %q", session2.AgentID, "beta")
	}
}

func TestFactory_Reload_ExistingSessionKeepsAgentID(t *testing.T) {
	t.Parallel()

	agentsA := map[string]AgentConfig{
		"old-agent": {
			Workspace: "/ws/old",
			Routing:   RoutingConfig{Default: true},
		},
	}
	regA, err := NewRegistry(agentsA, []string{"old-agent"})
	if err != nil {
		t.Fatalf("NewRegistry A: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry:        regA,
		DefaultProvider: newStubProvider(),
		GlobalTools:     newGlobalTools(t, "search"),
		Logger:          slog.Default(),
	})

	// Reload with registry that contains the same agent ID but different config.
	agentsB := map[string]AgentConfig{
		"old-agent": {
			Workspace: "/ws/new",
			Tools:     []string{"search"},
			Routing:   RoutingConfig{Default: true},
		},
	}
	regB, err := NewRegistry(agentsB, []string{"old-agent"})
	if err != nil {
		t.Fatalf("NewRegistry B: %v", err)
	}
	factory.Reload(regB)

	// Session with pre-set AgentID should keep it and use the new config.
	session := &router.Session{ID: "s1", AgentID: "old-agent"}
	msg := message.InboundMessage{
		Channel: "test",
		Sender:  message.Sender{ID: "user"},
		Chat:    message.Chat{ID: "chat"},
	}
	loop, err := factory.ForSession(session, msg)
	if err != nil {
		t.Fatalf("ForSession: %v", err)
	}
	if loop == nil {
		t.Fatal("expected non-nil loop")
	}
	if session.AgentID != "old-agent" {
		t.Errorf("AgentID = %q, want %q", session.AgentID, "old-agent")
	}
}

func TestFactory_Reload_InvalidatesSoulsOnDataDirChange(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	agentsA := map[string]AgentConfig{
		"bot": {
			DataDir: filepath.Join(tmpDir, "agents", "bot-v1"),
			Routing: RoutingConfig{Default: true},
		},
	}
	if err := EnsureDirectories(agentsA); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}
	regA, err := NewRegistry(agentsA, []string{"bot"})
	if err != nil {
		t.Fatalf("NewRegistry A: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry: regA,
		Logger:   slog.Default(),
	})

	// Populate the soul cache.
	_, err = factory.ResolveSoul("bot")
	if err != nil {
		t.Fatalf("ResolveSoul: %v", err)
	}

	factory.mu.RLock()
	_, cached := factory.souls["bot"]
	factory.mu.RUnlock()
	if !cached {
		t.Fatal("expected soul to be cached before reload")
	}

	// Reload with changed DataDir.
	agentsB := map[string]AgentConfig{
		"bot": {
			DataDir: filepath.Join(tmpDir, "agents", "bot-v2"),
			Routing: RoutingConfig{Default: true},
		},
	}
	if err := EnsureDirectories(agentsB); err != nil {
		t.Fatalf("EnsureDirectories B: %v", err)
	}
	regB, err := NewRegistry(agentsB, []string{"bot"})
	if err != nil {
		t.Fatalf("NewRegistry B: %v", err)
	}
	factory.Reload(regB)

	// Soul cache should be invalidated.
	factory.mu.RLock()
	_, cached = factory.souls["bot"]
	factory.mu.RUnlock()
	if cached {
		t.Error("expected soul cache to be invalidated after DataDir change")
	}
}

func TestFactory_Reload_InvalidatesStoresOnMemoryToggle(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	agentsA := map[string]AgentConfig{
		"bot": {
			DataDir: filepath.Join(tmpDir, "agents", "bot"),
			Routing: RoutingConfig{Default: true},
		},
	}
	ResolveDefaults(agentsA, tmpDir)
	if err := EnsureDirectories(agentsA); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}
	regA, err := NewRegistry(agentsA, []string{"bot"})
	if err != nil {
		t.Fatalf("NewRegistry A: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry: regA,
		Logger:   slog.Default(),
	})
	defer func() { _ = factory.Close() }()

	// Populate store cache.
	store := factory.ResolveHistory("bot")
	if store == nil {
		t.Fatal("expected non-nil store before reload")
	}

	// Reload with memory disabled.
	disabled := false
	agentsB := map[string]AgentConfig{
		"bot": {
			DataDir: filepath.Join(tmpDir, "agents", "bot"),
			Memory:  MemoryConfig{Enabled: &disabled},
			Routing: RoutingConfig{Default: true},
		},
	}
	regB, err := NewRegistry(agentsB, []string{"bot"})
	if err != nil {
		t.Fatalf("NewRegistry B: %v", err)
	}
	factory.Reload(regB)

	// Store cache should be invalidated.
	factory.mu.RLock()
	_, cached := factory.stores["bot"]
	factory.mu.RUnlock()
	if cached {
		t.Error("expected store cache to be invalidated after memory toggle")
	}
}

func TestFactory_Reload_PreservesUnchangedCaches(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	agents := map[string]AgentConfig{
		"stable": {
			DataDir: filepath.Join(tmpDir, "agents", "stable"),
			Routing: RoutingConfig{Default: true},
		},
	}
	ResolveDefaults(agents, tmpDir)
	if err := EnsureDirectories(agents); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}
	reg, err := NewRegistry(agents, []string{"stable"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry: reg,
		Logger:   slog.Default(),
	})
	defer func() { _ = factory.Close() }()

	// Populate caches.
	_, _ = factory.ResolveSoul("stable")
	_ = factory.ResolveHistory("stable")

	// Reload with identical config.
	reg2, err := NewRegistry(agents, []string{"stable"})
	if err != nil {
		t.Fatalf("NewRegistry 2: %v", err)
	}
	factory.Reload(reg2)

	// Both caches should still be populated.
	factory.mu.RLock()
	_, soulCached := factory.souls["stable"]
	_, storeCached := factory.stores["stable"]
	factory.mu.RUnlock()

	if !soulCached {
		t.Error("soul cache should be preserved for unchanged agent")
	}
	if !storeCached {
		t.Error("store cache should be preserved for unchanged agent")
	}
}

func TestFactory_Reload_RemovedAgent(t *testing.T) {
	t.Parallel()

	agentsA := map[string]AgentConfig{
		"existing": {
			Workspace: "/ws/existing",
			Routing:   RoutingConfig{Default: true},
		},
	}
	regA, err := NewRegistry(agentsA, []string{"existing"})
	if err != nil {
		t.Fatalf("NewRegistry A: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry:        regA,
		DefaultProvider: newStubProvider(),
		GlobalTools:     tool.NewRegistry(),
		Logger:          slog.Default(),
	})

	// Reload without the agent.
	agentsB := map[string]AgentConfig{
		"replacement": {
			Workspace: "/ws/replacement",
			Routing:   RoutingConfig{Default: true},
		},
	}
	regB, err := NewRegistry(agentsB, []string{"replacement"})
	if err != nil {
		t.Fatalf("NewRegistry B: %v", err)
	}
	factory.Reload(regB)

	// Session with removed agent should fail.
	session := &router.Session{ID: "s1", AgentID: "existing"}
	msg := message.InboundMessage{
		Channel: "test",
		Sender:  message.Sender{ID: "user"},
		Chat:    message.Chat{ID: "chat"},
	}
	_, err = factory.ForSession(session, msg)
	if !errors.Is(err, ErrAgentNotFound) {
		t.Errorf("ForSession error = %v, want %v", err, ErrAgentNotFound)
	}
}

func TestFactory_Reload_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	agents := map[string]AgentConfig{
		"bot": {
			Workspace: "/ws/bot",
			Routing:   RoutingConfig{Default: true},
		},
	}
	reg, err := NewRegistry(agents, []string{"bot"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	factory := NewFactory(FactoryConfig{
		Registry:        reg,
		DefaultProvider: newStubProvider(),
		GlobalTools:     newGlobalTools(t, "search"),
		Logger:          slog.Default(),
	})

	msg := message.InboundMessage{
		Channel: "test",
		Sender:  message.Sender{ID: "user"},
		Chat:    message.Chat{ID: "chat"},
	}

	// Run concurrent ForSession calls and reloads.
	// The -race detector validates correctness.
	done := make(chan struct{})
	const readers = 10
	const reloads = 5

	for i := range readers {
		go func(n int) {
			defer func() { done <- struct{}{} }()
			for j := range 20 {
				session := &router.Session{ID: fmt.Sprintf("s-%d-%d", n, j)}
				_, _ = factory.ForSession(session, msg)
			}
		}(i)
	}

	for range reloads {
		go func() {
			defer func() { done <- struct{}{} }()
			for range 4 {
				newReg, _ := NewRegistry(agents, []string{"bot"})
				factory.Reload(newReg)
			}
		}()
	}

	for range readers + reloads {
		<-done
	}
}
