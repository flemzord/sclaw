// Package configtool provides tools that let the agent read and modify
// the sclaw YAML configuration at runtime. Tools capture dependencies
// (config path, redactor, reload function) by closure, which is why they
// live in a dedicated package rather than in builtin/.
package configtool

import (
	"context"
	"fmt"

	"github.com/flemzord/sclaw/internal/security"
	"github.com/flemzord/sclaw/internal/tool"
)

// maxConfigSize is the maximum allowed size for a config file or patch (1 MiB).
const maxConfigSize = 1 << 20

// ReloadFunc is called after a successful config write to trigger
// in-process module reload. It receives the path to the updated file.
type ReloadFunc func(ctx context.Context, path string) error

// Deps holds the dependencies injected into config tools at registration time.
type Deps struct {
	// ConfigPath is the absolute path to the sclaw.yaml file.
	ConfigPath string

	// Redactor is used to redact secrets from config output.
	Redactor *security.Redactor

	// ReloadFn triggers a hot-reload of modules after a config write.
	ReloadFn ReloadFunc
}

// RegisterAll registers all config tools on the given registry.
func RegisterAll(registry *tool.Registry, deps Deps) error {
	tools := []tool.Tool{
		newGetTool(deps),
		newValidateTool(),
		newPatchTool(deps),
		newApplyTool(deps),
	}

	for _, t := range tools {
		if err := registry.Register(t); err != nil {
			return fmt.Errorf("registering config tool %s: %w", t.Name(), err)
		}
	}

	return nil
}
