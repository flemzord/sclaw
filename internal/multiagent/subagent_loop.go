package multiagent

import (
	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/subagent"
	"github.com/flemzord/sclaw/internal/tool"
)

// subAgentLoopFactory creates agent loops for sub-agents, adapting the
// application's provider and tool registry to the subagent.LoopFactory interface.
type subAgentLoopFactory struct {
	provider    provider.Provider
	globalTools *tool.Registry
}

// NewSubAgentLoopFactory returns a LoopFactory that builds agent loops using
// the given provider and a fresh copy of the global tools.
func NewSubAgentLoopFactory(p provider.Provider, tools *tool.Registry) subagent.LoopFactory {
	return &subAgentLoopFactory{provider: p, globalTools: tools}
}

func (f *subAgentLoopFactory) NewLoop(_ string) (*agent.Loop, error) {
	toolReg := f.globalTools
	if toolReg == nil {
		toolReg = tool.NewRegistry()
	}
	executor := agent.NewToolExecutor(agent.ToolExecutorConfig{Registry: toolReg})
	return agent.NewLoop(f.provider, executor, agent.LoopConfig{}), nil
}
