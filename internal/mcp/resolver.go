package mcp

import (
	"context"
	"log/slog"
	"sync"

	"github.com/flemzord/sclaw/internal/tool"
)

// resolvedAgent holds cached MCP clients and tools for a single agent.
// A nil value for clients indicates the agent has no mcp.json.
type resolvedAgent struct {
	clients []*Client
	tools   []tool.Tool
}

// Resolver manages per-agent MCP client lifecycles and caches resolved tools.
// It lazily connects to MCP servers on first access and caches the result.
type Resolver struct {
	logger *slog.Logger

	mu     sync.RWMutex
	agents map[string]*resolvedAgent
}

// NewResolver creates a new MCP tool resolver.
func NewResolver(logger *slog.Logger) *Resolver {
	return &Resolver{
		logger: logger,
		agents: make(map[string]*resolvedAgent),
	}
}

// ResolveTools returns the MCP tools available for the given agent.
// Results are cached per agentID. Returns nil if the agent has no mcp.json.
// Failed server connections are logged and skipped — the agent continues
// with whatever tools could be resolved.
func (r *Resolver) ResolveTools(ctx context.Context, agentID, dataDir string) []tool.Tool {
	if dataDir == "" {
		return nil
	}

	// Fast path: read lock.
	r.mu.RLock()
	if cached, ok := r.agents[agentID]; ok {
		r.mu.RUnlock()
		return cached.tools
	}
	r.mu.RUnlock()

	// Slow path: write lock + double-check.
	r.mu.Lock()
	defer r.mu.Unlock()

	if cached, ok := r.agents[agentID]; ok {
		return cached.tools
	}

	cfg, err := LoadConfig(dataDir)
	if err != nil {
		r.logger.Warn("mcp: failed to load config",
			"agent", agentID, "error", err)
		// Cache empty result to avoid repeated failed reads.
		r.agents[agentID] = &resolvedAgent{}
		return nil
	}

	if cfg == nil || len(cfg.Servers) == 0 {
		// No mcp.json or empty servers — cache nil result.
		r.agents[agentID] = &resolvedAgent{}
		return nil
	}

	var (
		clients []*Client
		tools   []tool.Tool
	)

	for name, serverCfg := range cfg.Servers {
		c := NewClient(name, serverCfg, r.logger)
		if err := c.Connect(ctx); err != nil {
			r.logger.Warn("mcp: failed to connect to server",
				"agent", agentID, "server", name, "error", err)
			continue
		}

		clients = append(clients, c)
		for _, mcpTool := range c.Tools() {
			tools = append(tools, NewTool(name, mcpTool, c))
		}
	}

	r.agents[agentID] = &resolvedAgent{
		clients: clients,
		tools:   tools,
	}

	if len(tools) > 0 {
		r.logger.Info("mcp: resolved tools for agent",
			"agent", agentID, "tools", len(tools), "servers", len(clients))
	}

	return tools
}

// InvalidateAgent closes all MCP clients for the given agent and removes
// it from the cache. The next ResolveTools call will re-read mcp.json
// and reconnect.
func (r *Resolver) InvalidateAgent(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cached, ok := r.agents[agentID]
	if !ok {
		return
	}

	for _, c := range cached.clients {
		if err := c.Close(); err != nil {
			r.logger.Warn("mcp: error closing client during invalidation",
				"agent", agentID, "error", err)
		}
	}
	delete(r.agents, agentID)
}

// Close closes all MCP clients managed by this resolver.
func (r *Resolver) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for agentID, cached := range r.agents {
		for _, c := range cached.clients {
			if err := c.Close(); err != nil && firstErr == nil {
				firstErr = err
				r.logger.Warn("mcp: error closing client",
					"agent", agentID, "error", err)
			}
		}
	}
	r.agents = make(map[string]*resolvedAgent)
	return firstErr
}
