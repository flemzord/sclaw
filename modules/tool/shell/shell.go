package shell

import (
	"fmt"

	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/tool"
	"gopkg.in/yaml.v3"
)

func init() {
	core.RegisterModule(&Module{})
}

// Compile-time interface guards.
var (
	_ core.Configurable = (*Module)(nil)
	_ core.Provisioner  = (*Module)(nil)
	_ core.Validator    = (*Module)(nil)
	_ tool.Provider     = (*Module)(nil)
)

// Module implements a configurable shell execution tool module.
type Module struct {
	config Config
	tool   *execTool
}

// ModuleInfo implements core.Module.
func (m *Module) ModuleInfo() core.ModuleInfo {
	return core.ModuleInfo{
		ID:  "tool.shell",
		New: func() core.Module { return &Module{} },
	}
}

// Configure implements core.Configurable.
func (m *Module) Configure(node *yaml.Node) error {
	if err := node.Decode(&m.config); err != nil {
		return fmt.Errorf("shell: decode config: %w", err)
	}
	return nil
}

// Provision implements core.Provisioner.
func (m *Module) Provision(_ *core.AppContext) error {
	m.config.defaults()

	m.tool = &execTool{
		timeout:    m.config.timeoutDuration(),
		maxTimeout: m.config.maxTimeoutDuration(),
		maxOutput:  m.config.MaxOutputSize,
		policy:     tool.ApprovalLevel(m.config.DefaultPolicy),
	}
	return nil
}

// Validate implements core.Validator.
func (m *Module) Validate() error {
	return m.config.validate()
}

// Tools implements tool.Provider.
func (m *Module) Tools() []tool.Tool {
	return []tool.Tool{m.tool}
}
