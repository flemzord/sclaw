package multiagent

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/router"
	"github.com/flemzord/sclaw/internal/security"
	"github.com/flemzord/sclaw/internal/tool"
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
// with the correct provider, tools, and workspace.
type Factory struct {
	cfg FactoryConfig
}

// Compile-time check.
var _ router.AgentFactory = (*Factory)(nil)

// NewFactory creates a Factory from the given configuration.
func NewFactory(cfg FactoryConfig) *Factory {
	return &Factory{cfg: cfg}
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
			DataDir:      agentCfg.Workspace + "/data",
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
