// Package core provides the module system foundation for sclaw.
package core

import (
	"fmt"
	"log/slog"

	"gopkg.in/yaml.v3"
)

// AppContext carries shared resources available to modules during provisioning
// and at runtime.
type AppContext struct {
	// Logger for the current module scope.
	Logger *slog.Logger

	// DataDir is the root directory for persistent module data.
	DataDir string

	// Workspace is the working directory for the current agent/session.
	Workspace string

	parentLogger  *slog.Logger
	moduleConfigs map[string]yaml.Node
}

// NewAppContext creates a new AppContext with the given base logger and directories.
func NewAppContext(logger *slog.Logger, dataDir, workspace string) *AppContext {
	if logger == nil {
		logger = slog.Default()
	}
	return &AppContext{
		Logger:       logger,
		DataDir:      dataDir,
		Workspace:    workspace,
		parentLogger: logger,
	}
}

// WithModuleConfigs returns a copy of the AppContext with module configurations set.
// Each key is a module ID mapping to its raw YAML configuration node.
func (ctx *AppContext) WithModuleConfigs(configs map[string]yaml.Node) *AppContext {
	cp := *ctx
	cp.moduleConfigs = configs
	return &cp
}

// ForModule returns a new AppContext scoped to the given module ID,
// with a child logger that includes the module ID.
func (ctx *AppContext) ForModule(id ModuleID) *AppContext {
	return &AppContext{
		Logger:        ctx.parentLogger.With("module", string(id)),
		DataDir:       ctx.DataDir,
		Workspace:     ctx.Workspace,
		parentLogger:  ctx.parentLogger,
		moduleConfigs: ctx.moduleConfigs,
	}
}

// LoadModule instantiates and provisions a module by its ID.
// It calls Configure, Provision and Validate if the module implements
// those interfaces. The lifecycle order is:
//
//	New() → Configure() → Provision() → Validate()
//
// Returns the provisioned module instance ready for use.
func (ctx *AppContext) LoadModule(id string) (Module, error) {
	info, ok := GetModule(id)
	if !ok {
		return nil, fmt.Errorf("unknown module: %s", id)
	}

	mod := info.New()

	if c, ok := mod.(Configurable); ok {
		if node, exists := ctx.moduleConfigs[id]; exists {
			if err := c.Configure(&node); err != nil {
				return nil, fmt.Errorf("configuring module %s: %w", id, err)
			}
		}
	}

	if p, ok := mod.(Provisioner); ok {
		moduleCtx := ctx.ForModule(info.ID)
		if err := p.Provision(moduleCtx); err != nil {
			return nil, fmt.Errorf("provisioning module %s: %w", id, err)
		}
	}

	if v, ok := mod.(Validator); ok {
		if err := v.Validate(); err != nil {
			return nil, fmt.Errorf("validating module %s: %w", id, err)
		}
	}

	return mod, nil
}
