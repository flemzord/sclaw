// Package crontool provides CRUD tools for managing prompt cron definitions.
// The agent can list, get, create, update, and delete scheduled prompts
// stored as JSON files in the crons directory.
package crontool

import (
	"fmt"

	"github.com/flemzord/sclaw/internal/tool"
)

// ReloadFunc is called after a cron mutation to trigger a scheduler re-scan.
type ReloadFunc func() error

// Deps holds the dependencies injected into cron tools at registration time.
type Deps struct {
	// ReloadFn triggers a scheduler re-scan after a cron mutation (create/update/delete).
	ReloadFn ReloadFunc
}

// RegisterAll registers all cron CRUD tools on the given registry.
func RegisterAll(registry *tool.Registry, deps Deps) error {
	tools := []tool.Tool{
		newListTool(),
		newGetTool(),
		newCreateTool(deps),
		newUpdateTool(deps),
		newDeleteTool(deps),
	}

	for _, t := range tools {
		if err := registry.Register(t); err != nil {
			return fmt.Errorf("registering cron tool %s: %w", t.Name(), err)
		}
	}

	return nil
}
