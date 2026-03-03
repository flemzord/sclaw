package crontool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/flemzord/sclaw/internal/cron"
	"github.com/flemzord/sclaw/internal/tool"
)

type createTool struct {
	deps Deps
}

func newCreateTool(deps Deps) tool.Tool { return &createTool{deps: deps} }

func (t *createTool) Name() string         { return "cron_create" }
func (t *createTool) Description() string  { return "Create a new prompt cron definition." }
func (t *createTool) Scopes() []tool.Scope { return []tool.Scope{tool.ScopeReadWrite} }
func (t *createTool) DefaultPolicy() tool.ApprovalLevel {
	return tool.ApprovalAsk
}

func (t *createTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name":        {"type": "string", "description": "Unique name for the cron (alphanumeric, hyphens, underscores)."},
			"description": {"type": "string", "description": "Human-readable description."},
			"schedule":    {"type": "string", "description": "5-field cron expression (e.g. '0 9 * * *')."},
			"enabled":     {"type": "boolean", "description": "Whether the cron is active."},
			"prompt":      {"type": "string", "description": "The prompt to execute."},
			"tools":       {"type": "array", "items": {"type": "string"}, "description": "Optional tool filter."},
			"loop":        {"type": "object", "properties": {"max_iterations": {"type": "integer"}, "timeout": {"type": "string"}}, "description": "Optional loop config overrides."},
			"output":      {"type": "object", "properties": {"channel": {"type": "string"}, "chat_id": {"type": "string"}}, "description": "Optional output destination."}
		},
		"required": ["name", "schedule", "prompt"],
		"additionalProperties": false
	}`)
}

func (t *createTool) Execute(_ context.Context, args json.RawMessage, env tool.ExecutionEnv) (tool.Output, error) {
	var def cron.PromptCronDef
	if err := json.Unmarshal(args, &def); err != nil {
		return tool.Output{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	if !isValidCronName(def.Name) {
		return tool.Output{Content: fmt.Sprintf("invalid cron name: %q (must be alphanumeric, hyphens, underscores, max %d chars)", def.Name, maxCronNameLen), IsError: true}, nil
	}

	if err := def.Validate(); err != nil {
		return tool.Output{Content: fmt.Sprintf("validation error: %v", err), IsError: true}, nil
	}

	// Ensure directory exists.
	dir := cron.CronsDir(env.DataDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return tool.Output{Content: fmt.Sprintf("failed to create crons dir: %v", err), IsError: true}, nil
	}

	// Check for existing cron with same name.
	path := filepath.Join(dir, def.Name+".json")
	if _, err := os.Stat(path); err == nil {
		return tool.Output{Content: fmt.Sprintf("cron %q already exists", def.Name), IsError: true}, nil
	}

	// Write definition.
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return tool.Output{Content: fmt.Sprintf("failed to marshal definition: %v", err), IsError: true}, nil
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return tool.Output{Content: fmt.Sprintf("failed to write file: %v", err), IsError: true}, nil
	}

	// Trigger reload so the scheduler picks up the new cron.
	if t.deps.ReloadFn != nil {
		if err := t.deps.ReloadFn(); err != nil {
			return tool.Output{Content: fmt.Sprintf("cron created but reload failed: %v", err), IsError: true}, nil
		}
	}

	return tool.Output{Content: fmt.Sprintf("prompt cron %q created successfully", def.Name)}, nil
}
