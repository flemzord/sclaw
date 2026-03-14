package multiagent

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/mcp"
	"github.com/flemzord/sclaw/internal/memory"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/router"
	"github.com/flemzord/sclaw/internal/security"
	"github.com/flemzord/sclaw/internal/subagent"
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

	// BuiltinSkillsFS, if non-nil, provides an embedded filesystem of
	// skills compiled into the binary. These skills are available by
	// default but can be overridden by global or per-agent filesystem skills
	// with the same name.
	BuiltinSkillsFS fs.FS

	// GlobalSkillsDir is the path to the global skills directory.
	// Skills in this directory are available to all agents by default.
	GlobalSkillsDir string
}

// Factory resolves the agent for a session and creates an agent.Loop
// with the correct provider, tools, and workspace. It also acts as a
// HistoryResolver and SoulResolver, lazily opening per-agent resources.
type Factory struct {
	cfg FactoryConfig

	// registry holds the current immutable Registry, swapped atomically on reload.
	registry atomic.Pointer[Registry]

	subAgentMgr *subagent.Manager
	mcpResolver *mcp.Resolver

	mu         sync.RWMutex
	stores     map[string]memory.HistoryStore
	factStores map[string]memory.Store
	souls      map[string]workspace.SoulProvider
	dbs        []*sql.DB
}

// Compile-time checks.
var (
	_ router.AgentFactory    = (*Factory)(nil)
	_ router.HistoryResolver = (*Factory)(nil)
	_ router.SoulResolver    = (*Factory)(nil)
	_ router.SkillResolver   = (*Factory)(nil)
)

// NewFactory creates a Factory from the given configuration.
func NewFactory(cfg FactoryConfig) *Factory {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	f := &Factory{
		cfg:         cfg,
		mcpResolver: mcp.NewResolver(logger),
		stores:      make(map[string]memory.HistoryStore),
		factStores:  make(map[string]memory.Store),
		souls:       make(map[string]workspace.SoulProvider),
	}
	f.registry.Store(cfg.Registry)
	return f
}

// SetSubAgentManager sets the sub-agent manager on the factory.
// This uses a setter to break the circular dependency: Factory needs Manager
// for tool registration, but Manager needs a LoopFactory that uses the provider
// owned by Factory.
func (f *Factory) SetSubAgentManager(mgr *subagent.Manager) {
	f.subAgentMgr = mgr
}

// currentRegistry returns the current immutable Registry.
// Uses atomic load for lock-free access on the hot path (ForSession).
func (f *Factory) currentRegistry() *Registry {
	return f.registry.Load()
}

// GlobalTools returns the global tool registry.
func (f *Factory) GlobalTools() *tool.Registry {
	return f.cfg.GlobalTools
}

// ForSession resolves the agent for the session and builds an agent.Loop.
// If the session has no AgentID yet, it is resolved from the inbound message
// and stored back on the session.
func (f *Factory) ForSession(session *router.Session, msg message.InboundMessage) (*agent.Loop, error) {
	agentID := session.AgentID
	if agentID == "" {
		resolved, err := f.currentRegistry().Resolve(msg)
		if err != nil {
			return nil, fmt.Errorf("multiagent: resolving agent for session %s: %w", session.ID, err)
		}
		agentID = resolved
		session.AgentID = agentID
	}

	agentCfg, ok := f.currentRegistry().AgentConfig(agentID)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrAgentNotFound, agentID)
	}

	// Propagate streaming flag to the session.
	session.StreamingEnabled = agentCfg.IsStreamingEnabled()

	// Resolve provider.
	// Note: custom provider resolution would go here in the future.
	// For now, all agents use the default provider.
	p := f.cfg.DefaultProvider
	_ = agentCfg.Provider // acknowledged but not resolved yet

	// Build tool registry (filtered or global).
	toolReg := f.buildToolRegistry(agentCfg)

	// Inject per-agent MCP tools from mcp.json.
	mcpTools := f.mcpResolver.ResolveTools(context.Background(), agentID, agentCfg.DataDir)
	if len(mcpTools) > 0 {
		// Clone if we got the shared global registry to avoid mutating it.
		if toolReg == f.cfg.GlobalTools {
			toolReg = f.cfg.GlobalTools.Clone()
		}
		for _, t := range mcpTools {
			if len(agentCfg.Tools) > 0 && !slices.Contains(agentCfg.Tools, t.Name()) {
				continue
			}
			_ = toolReg.Register(t)
		}
	}

	// Register sub-agent tools so the session can spawn/manage sub-agents.
	// Skip if already registered (ForSession is called per-message and the
	// global tool registry persists across calls).
	if f.subAgentMgr != nil {
		if _, err := toolReg.Get("sessions_list"); err != nil {
			if err := subagent.RegisterTools(toolReg, f.subAgentMgr, session.AgentID, session.ID, false); err != nil {
				return nil, fmt.Errorf("multiagent: registering subagent tools for session %s: %w", session.ID, err)
			}
		}
	}

	// Wire audit logger and rate limiter into the tool registry so that
	// every tool call is recorded and rate-limited per-session.
	if f.cfg.AuditLogger != nil {
		toolReg.SetAuditLogger(f.cfg.AuditLogger)
	}
	if f.cfg.RateLimiter != nil {
		toolReg.SetRateLimiter(f.cfg.RateLimiter)
	}

	// Build path filter for allowed directories outside workspace.
	var pathFilter *security.PathFilter
	if len(agentCfg.AllowedDirs) > 0 {
		dirs := make([]security.AllowedDir, len(agentCfg.AllowedDirs))
		for i, d := range agentCfg.AllowedDirs {
			dirs[i] = security.AllowedDir{
				Path: d.Path,
				Mode: security.PathAccessMode(d.Mode),
			}
		}
		pathFilter = security.NewPathFilter(security.PathFilterConfig{AllowedDirs: dirs})
	}

	// Build executor.
	executor := agent.NewToolExecutor(agent.ToolExecutorConfig{
		Registry: toolReg,
		Env: tool.ExecutionEnv{
			Workspace:    agentCfg.Workspace,
			DataDir:      agentCfg.DataDir,
			SanitizedEnv: f.cfg.SanitizedEnv,
			URLFilter:    f.cfg.URLFilter,
			PathFilter:   pathFilter,
			SessionID:    session.ID,
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

	agentCfg, ok := f.currentRegistry().AgentConfig(agentID)
	if !ok || !agentCfg.Memory.IsEnabled() || agentCfg.DataDir == "" {
		// Cache nil to avoid repeated lookups.
		f.stores[agentID] = nil
		return nil
	}

	// Per-agent SQLite: opens both history and fact stores from a single DB.
	dbPath := filepath.Join(agentCfg.DataDir, "memory.db")

	logger := f.cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	histStore, factStore, db, err := sqlite.OpenStores(dbPath, logger)
	if err != nil {
		if f.cfg.Logger != nil {
			f.cfg.Logger.Error("multiagent: failed to open memory stores",
				"agent", agentID, "path", dbPath, "error", err)
		}
		f.stores[agentID] = nil
		return nil
	}

	f.stores[agentID] = histStore
	f.factStores[agentID] = factStore
	f.dbs = append(f.dbs, db)

	if f.cfg.Logger != nil {
		f.cfg.Logger.Info("multiagent: opened per-agent memory stores",
			"agent", agentID, "path", dbPath)
	}
	return histStore
}

// ResolveFactStore returns the persistent fact Store for the given agent.
// Returns nil if memory is disabled or the agent has no DataDir.
// The store is lazily opened on first access (via ResolveHistory) and cached.
func (f *Factory) ResolveFactStore(agentID string) memory.Store {
	// Fast path: read lock.
	f.mu.RLock()
	if s, ok := f.factStores[agentID]; ok {
		f.mu.RUnlock()
		return s
	}
	f.mu.RUnlock()

	// Trigger lazy open of both stores via ResolveHistory.
	f.ResolveHistory(agentID)

	// Read back the cached fact store.
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.factStores[agentID]
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

	agentCfg, ok := f.currentRegistry().AgentConfig(agentID)
	if !ok || agentCfg.DataDir == "" {
		return workspace.DefaultSoulPrompt, nil
	}

	soulPath := filepath.Join(agentCfg.DataDir, "SOUL.md")
	loader := workspace.NewSoulLoader(soulPath)
	f.souls[agentID] = loader
	return loader.Load()
}

// ResolveSkills returns the formatted skill section for the given agent.
// It loads global skills from GlobalSkillsDir, per-agent skills from the
// agent's data directory, filters excluded global skills, activates based
// on trigger rules and available tools, then formats for the system prompt.
func (f *Factory) ResolveSkills(agentID, userMessage string) (string, error) {
	logger := f.cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	agentCfg, ok := f.currentRegistry().AgentConfig(agentID)
	if !ok {
		return "", nil
	}

	// Load builtin skills from embedded FS.
	builtinSkills, err := workspace.LoadSkillsFromFS(f.cfg.BuiltinSkillsFS, workspace.BuiltinPathPrefix)
	if err != nil {
		return "", fmt.Errorf("multiagent: loading builtin skills: %w", err)
	}

	// Load global filesystem skills.
	globalSkills, err := workspace.LoadSkillsFromDir(f.cfg.GlobalSkillsDir)
	if err != nil {
		return "", fmt.Errorf("multiagent: loading global skills: %w", err)
	}

	// Merge builtin + global (global overrides builtin by name).
	merged := workspace.MergeSkills(builtinSkills, globalSkills)

	// Filter excluded skills from the merged set.
	merged = workspace.ExcludeByName(merged, agentCfg.ExcludeSkills)

	// Load per-agent skills.
	agentSkillsDir := filepath.Join(agentCfg.DataDir, "skills")
	agentSkills, err := workspace.LoadSkillsFromDir(agentSkillsDir)
	if err != nil {
		return "", fmt.Errorf("multiagent: loading agent skills for %q: %w", agentID, err)
	}

	logger.Debug("skills loaded",
		"agent_id", agentID,
		"builtin_count", len(builtinSkills),
		"global_count", len(globalSkills),
		"global_dir", f.cfg.GlobalSkillsDir,
		"merged_count", len(merged),
		"agent_count", len(agentSkills),
		"agent_dir", agentSkillsDir,
	)

	// Final merge: merged (builtin+global) + per-agent.
	allSkills := slices.Concat(merged, agentSkills)
	if len(allSkills) == 0 {
		logger.Debug("no skills found for agent", "agent_id", agentID)
		return "", nil
	}

	// Get available tool names.
	toolReg := f.buildToolRegistry(agentCfg)
	toolNames := toolReg.Names()

	// Activate skills based on trigger rules and available tools.
	active := workspace.NewSkillActivator().Activate(workspace.ActivateRequest{
		Skills:         allSkills,
		UserMessage:    userMessage,
		AvailableTools: toolNames,
	})

	activeNames := make([]string, len(active))
	for i, s := range active {
		activeNames[i] = s.Meta.Name
	}

	logger.Debug("skills activation complete",
		"agent_id", agentID,
		"loaded", len(allSkills),
		"active", len(active),
		"active_names", activeNames,
	)

	return workspace.FormatSkillsForPrompt(active), nil
}

// ForCronJob builds an agent.Loop for cron execution with allow-all policy.
// Unlike ForSession, it does not require a router.Session and uses a permissive
// policy (all tools auto-approved) since cron jobs are system-initiated.
func (f *Factory) ForCronJob(agentID string, toolFilter []string, loopOverrides agent.LoopConfig) (*agent.Loop, string, error) {
	agentCfg, ok := f.currentRegistry().AgentConfig(agentID)
	if !ok {
		return nil, "", fmt.Errorf("%w: %q", ErrAgentNotFound, agentID)
	}

	p := f.cfg.DefaultProvider

	// Build tool registry filtered by agent allowlist.
	toolReg := f.buildToolRegistry(agentCfg)

	// Inject per-agent MCP tools from mcp.json.
	mcpTools := f.mcpResolver.ResolveTools(context.Background(), agentID, agentCfg.DataDir)
	if len(mcpTools) > 0 {
		if toolReg == f.cfg.GlobalTools {
			toolReg = f.cfg.GlobalTools.Clone()
		}
		for _, t := range mcpTools {
			if len(agentCfg.Tools) > 0 && !slices.Contains(agentCfg.Tools, t.Name()) {
				continue
			}
			_ = toolReg.Register(t)
		}
	}

	// Apply additional cron-specific tool filter (intersection).
	if len(toolFilter) > 0 {
		filtered := tool.NewRegistry()
		for _, name := range toolFilter {
			if t, err := toolReg.Get(name); err == nil {
				_ = filtered.Register(t)
			}
		}
		toolReg = filtered
	}

	// Wire audit logger but NOT rate limiter (cron = system, not user).
	if f.cfg.AuditLogger != nil {
		toolReg.SetAuditLogger(f.cfg.AuditLogger)
	}

	// Build path filter for allowed directories outside workspace.
	var pathFilter *security.PathFilter
	if len(agentCfg.AllowedDirs) > 0 {
		dirs := make([]security.AllowedDir, len(agentCfg.AllowedDirs))
		for i, d := range agentCfg.AllowedDirs {
			dirs[i] = security.AllowedDir{
				Path: d.Path,
				Mode: security.PathAccessMode(d.Mode),
			}
		}
		pathFilter = security.NewPathFilter(security.PathFilterConfig{AllowedDirs: dirs})
	}

	// Build executor with allow-all policy (cron jobs need no user approval).
	executor := agent.NewToolExecutor(agent.ToolExecutorConfig{
		Registry: toolReg,
		PolicyCfg: tool.PolicyConfig{
			DM: tool.Policy{Default: tool.ApprovalAllow},
		},
		PolicyCtx: tool.PolicyContextDM,
		Env: tool.ExecutionEnv{
			Workspace:    agentCfg.Workspace,
			DataDir:      agentCfg.DataDir,
			SanitizedEnv: f.cfg.SanitizedEnv,
			URLFilter:    f.cfg.URLFilter,
			PathFilter:   pathFilter,
		},
	})

	// Build loop config: agent defaults merged with cron overrides.
	loopCfg := f.buildLoopConfig(agentCfg)
	if loopOverrides.MaxIterations > 0 {
		loopCfg.MaxIterations = loopOverrides.MaxIterations
	}
	if loopOverrides.Timeout > 0 {
		loopCfg.Timeout = loopOverrides.Timeout
	}

	// Resolve system prompt.
	systemPrompt, err := f.ResolveSoul(agentID)
	if err != nil {
		return nil, "", fmt.Errorf("multiagent: resolving soul for cron %q: %w", agentID, err)
	}

	return agent.NewLoop(p, executor, loopCfg), systemPrompt, nil
}

// BuildCronLoop implements cron.LoopBuilder.
func (f *Factory) BuildCronLoop(agentID string, toolFilter []string, loopOverrides agent.LoopConfig) (*agent.Loop, string, error) {
	return f.ForCronJob(agentID, toolFilter, loopOverrides)
}

// Reload atomically swaps the Registry and selectively invalidates caches
// for agents whose configuration has changed. In-flight messages that already
// loaded the old Registry via atomic.Load continue unaffected; the next
// ForSession call will use the new Registry.
func (f *Factory) Reload(newRegistry *Registry) {
	old := f.registry.Swap(newRegistry)

	// Invalidate MCP clients for deleted agents or agents whose DataDir changed.
	// Done before acquiring f.mu since mcpResolver has its own lock.
	for _, agentID := range old.AgentIDs() {
		oldCfg, oldOK := old.AgentConfig(agentID)
		newCfg, newOK := newRegistry.AgentConfig(agentID)
		if !newOK || (oldOK && oldCfg.DataDir != newCfg.DataDir) {
			f.mcpResolver.InvalidateAgent(agentID)
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// Build a set of new agent IDs for quick lookup.
	newIDs := make(map[string]struct{}, len(newRegistry.AgentIDs()))
	for _, id := range newRegistry.AgentIDs() {
		newIDs[id] = struct{}{}
	}

	// Invalidate souls cache: remove entries for deleted agents or agents
	// whose DataDir changed (which changes the SOUL.md path).
	for agentID := range f.souls {
		oldCfg, oldOK := old.AgentConfig(agentID)
		newCfg, newOK := newRegistry.AgentConfig(agentID)
		if !newOK || (oldOK && oldCfg.DataDir != newCfg.DataDir) {
			delete(f.souls, agentID)
		}
	}

	// Invalidate stores cache: remove entries for deleted agents, agents
	// whose DataDir changed, or agents whose memory enabled state changed.
	for agentID := range f.stores {
		oldCfg, oldOK := old.AgentConfig(agentID)
		newCfg, newOK := newRegistry.AgentConfig(agentID)
		if !newOK {
			delete(f.stores, agentID)
			delete(f.factStores, agentID)
			continue
		}
		if oldOK && (oldCfg.DataDir != newCfg.DataDir || oldCfg.Memory.IsEnabled() != newCfg.Memory.IsEnabled()) {
			delete(f.stores, agentID)
			delete(f.factStores, agentID)
		}
	}

	if f.cfg.Logger != nil {
		f.cfg.Logger.Info("multiagent: registry reloaded",
			"old_agents", old.AgentIDs(),
			"new_agents", newRegistry.AgentIDs(),
		)
	}
}

// Close closes all MCP clients and SQLite databases.
func (f *Factory) Close() error {
	// Close MCP resolver first (has its own lock).
	mcpErr := f.mcpResolver.Close()

	f.mu.Lock()
	defer f.mu.Unlock()

	var firstErr error
	if mcpErr != nil {
		firstErr = mcpErr
	}
	for _, db := range f.dbs {
		if err := db.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	f.dbs = nil
	f.stores = make(map[string]memory.HistoryStore)
	f.factStores = make(map[string]memory.Store)
	return firstErr
}
