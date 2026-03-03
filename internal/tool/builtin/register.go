package builtin

import (
	"github.com/flemzord/sclaw/internal/tool"
)

// RegisterAll registers all built-in tools on the given registry.
func RegisterAll(registry *tool.Registry) error {
	for _, t := range All() {
		if err := registry.Register(t); err != nil {
			return err
		}
	}
	return nil
}

// All returns all built-in tool instances without registering them.
// Used by wire.go for granular fallback: only tools not already provided
// by a ToolProvider module are registered from this list.
func All() []tool.Tool {
	return []tool.Tool{
		&execTool{},
		&readFileTool{},
		&writeFileTool{},
	}
}
