package builtin

import (
	"github.com/flemzord/sclaw/internal/tool"
)

// RegisterAll registers all built-in tools on the given registry.
func RegisterAll(registry *tool.Registry) error {
	tools := []tool.Tool{
		&execTool{},
		&readFileTool{},
		&writeFileTool{},
	}

	for _, t := range tools {
		if err := registry.Register(t); err != nil {
			return err
		}
	}

	return nil
}
