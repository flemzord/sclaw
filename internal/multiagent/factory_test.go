package multiagent

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/provider/providertest"
	"github.com/flemzord/sclaw/internal/router"
	"github.com/flemzord/sclaw/internal/tool"
	"github.com/flemzord/sclaw/internal/tool/tooltest"
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
