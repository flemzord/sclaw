package multiagent

import (
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/memory"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/router"
	"github.com/flemzord/sclaw/internal/security"
	"github.com/flemzord/sclaw/internal/tool"
	"github.com/flemzord/sclaw/internal/workspace"
	"github.com/flemzord/sclaw/modules/memory/sqlite"
	"github.com/flemzord/sclaw/pkg/message"
)

// FactoryConfig holds dependencies for MultiAgentFactory.
type FactoryConfig struct {
	Registry        *Registry
	DefaultProvider provider.Provider
	GlobalTools     *tool.Registry
	Logger          *slog.Logger

	// AuditLogger, if non-nil, is wired into each per-session tool registry
	// so that tool executions are recorded in the audit log.
	AuditLogger *security.AuditLogger

	// RateLimiter, if non-nil, is wired into each per-session tool registry
	// to enforce tool-call rate limits.
	RateLimiter *security.RateLimiter

	// URLFilter, if non-nil, restricts which URLs network tools can access.
	URLFilter *security.URLFilter

	// SanitizedEnv, if non-nil, provides a pre-sanitized set of environment
	// variables passed to tools that spawn subprocesses.
	SanitizedEnv []string
}

// Factory resolves the agent for a session and creates an agent.Loop
// with the correct provider, tools, and workspace. It also acts as a
// HistoryResolver and SoulResolver, lazily opening per-agent resources.
type Factory struct {
	cfg FactoryConfig

	mu     sync.RWMutex
	stores map[string]memory.HistoryStore
	souls  map[string]workspace.SoulProvider
	dbs    []*sql.DB
}

// Compile-time checks.
var (
	_ router.AgentFactory    = (*Factory)(nil)
	_ router.HistoryResolver = (*Factory)(nil)
	_ router.SoulResolver    = (*Factory)(nil)
)

// NewFactory creates a Factory from the given configuration.
func NewFactory(cfg FactoryConfig) *Factory {
	return &Factory{
		cfg:    cfg,
		stores: make(map[string]memory.HistoryStore),
		souls:  make(map[string]workspace.SoulProvider),
	}
}

// ForSession resolves the agent for the session and builds an agent.Loop.
// If the session has no AgentID yet, it is resolved from the inbound message
// and stored back on the session.
func (f *Factory) ForSession(session *router.Session, msg message.InboundMessage) (*agent.Loop, error) {
	agentID := session.AgentID
	if agentID == "" {
		resolved, err := f.cfg.Registry.Resolve(msg)
		if err != nil {
			return nil, fmt.Errorf("multiagent: resolving agent for session %s: %w", session.ID, err)
		}
		agentID = resolved
		session.AgentID = agentID
	}

	agentCfg, ok := f.cfg.Registry.AgentConfig(agentID)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrAgentNotFound, agentID)
	}

	// Resolve provider.
	// Note: custom provider resolution would go here in the future.
	// For now, all agents use the default provider.
	p := f.cfg.DefaultProvider
	_ = agentCfg.Provider // acknowledged but not resolved yet

	// Build tool registry (filtered or global).
	toolReg := f.buildToolRegistry(agentCfg)

	// Wire audit logger and rate limiter into the tool registry so that
	// every tool call is recorded and rate-limited per-session.
	if f.cfg.AuditLogger != nil {
		toolReg.SetAuditLogger(f.cfg.AuditLogger)
	}
	if f.cfg.RateLimiter != nil {
		toolReg.SetRateLimiter(f.cfg.RateLimiter)
	}

	// Build executor.
	executor := agent.NewToolExecutor(agent.ToolExecutorConfig{
		Registry: toolReg,
		Env: tool.ExecutionEnv{
			Workspace:    agentCfg.Workspace,
			DataDir:      agentCfg.DataDir,
			SanitizedEnv: f.cfg.SanitizedEnv,
			URLFilter:    f.cfg.URLFilter,
		},
	})

	// Build loop config with per-agent overrides.
	loopCfg := f.buildLoopConfig(agentCfg)

	return agent.NewLoop(p, executor, loopCfg), nil
}

// buildToolRegistry returns a filtered tool registry when the agent specifies
// a tool allowlist, or the global registry otherwise.
func (f *Factory) buildToolRegistry(cfg AgentConfig) *tool.Registry {
	if len(cfg.Tools) == 0 {
		return f.cfg.GlobalTools
	}
	filtered := tool.NewRegistry()
	for _, name := range cfg.Tools {
		if t, err := f.cfg.GlobalTools.Get(name); err == nil {
			_ = filtered.Register(t)
		}
	}
	return filtered
}

// buildLoopConfig converts per-agent overrides into an agent.LoopConfig.
// Zero values are left for the Loop's withDefaults to fill.
func (f *Factory) buildLoopConfig(cfg AgentConfig) agent.LoopConfig {
	lc := agent.LoopConfig{
		MaxIterations: cfg.Loop.MaxIterations,
		TokenBudget:   cfg.Loop.TokenBudget,
		LoopThreshold: cfg.Loop.LoopThreshold,
	}
	if cfg.Loop.Timeout != "" {
		if d, err := time.ParseDuration(cfg.Loop.Timeout); err == nil {
			lc.Timeout = d
		}
	}
	return lc
}

// ResolveHistory returns the persistent HistoryStore for the given agent.
// Returns nil if memory is disabled or the agent has no DataDir.
// The store is lazily opened on first access and cached for subsequent calls.
func (f *Factory) ResolveHistory(agentID string) memory.HistoryStore {
	// Fast path: read lock.
	f.mu.RLock()
	if s, ok := f.stores[agentID]; ok {
		f.mu.RUnlock()
		return s
	}
	f.mu.RUnlock()

	// Slow path: write lock + double-check.
	f.mu.Lock()
	defer f.mu.Unlock()

	if s, ok := f.stores[agentID]; ok {
		return s
	}

	agentCfg, ok := f.cfg.Registry.AgentConfig(agentID)
	if !ok || !agentCfg.Memory.IsEnabled() || agentCfg.DataDir == "" {
		// Cache nil to avoid repeated lookups.
		f.stores[agentID] = nil
		return nil
	}

	dbPath := filepath.Join(agentCfg.DataDir, "memory.db")
	store, db, err := sqlite.OpenHistoryStore(dbPath)
	if err != nil {
		if f.cfg.Logger != nil {
			f.cfg.Logger.Error("multiagent: failed to open history store",
				"agent", agentID, "path", dbPath, "error", err)
		}
		f.stores[agentID] = nil
		return nil
	}

	f.stores[agentID] = store
	f.dbs = append(f.dbs, db)

	if f.cfg.Logger != nil {
		f.cfg.Logger.Info("multiagent: opened history store",
			"agent", agentID, "path", dbPath)
	}
	return store
}

// ResolveSoul returns the system prompt for the given agent.
// The SoulLoader is lazily created on first access and cached for subsequent calls.
// Returns the default prompt if the agent has no DataDir or no SOUL.md file.
func (f *Factory) ResolveSoul(agentID string) (string, error) {
	// Fast path: read lock.
	f.mu.RLock()
	if loader, ok := f.souls[agentID]; ok {
		f.mu.RUnlock()
		return loader.Load()
	}
	f.mu.RUnlock()

	// Slow path: write lock + double-check.
	f.mu.Lock()
	defer f.mu.Unlock()

	if loader, ok := f.souls[agentID]; ok {
		return loader.Load()
	}

	agentCfg, ok := f.cfg.Registry.AgentConfig(agentID)
	if !ok || agentCfg.DataDir == "" {
		return workspace.DefaultSoulPrompt, nil
	}

	soulPath := filepath.Join(agentCfg.DataDir, "SOUL.md")
	loader := workspace.NewSoulLoader(soulPath)
	f.souls[agentID] = loader
	return loader.Load()
}

// Close closes all SQLite databases opened by ResolveHistory.
func (f *Factory) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	var firstErr error
	for _, db := range f.dbs {
		if err := db.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	f.dbs = nil
	f.stores = make(map[string]memory.HistoryStore)
	return firstErr
}
