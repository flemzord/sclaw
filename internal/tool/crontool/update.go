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

type updateTool struct {
	deps Deps
}

func newUpdateTool(deps Deps) tool.Tool { return &updateTool{deps: deps} }

func (t *updateTool) Name() string         { return "cron_update" }
func (t *updateTool) Description() string  { return "Update an existing prompt cron definition." }
func (t *updateTool) Scopes() []tool.Scope { return []tool.Scope{tool.ScopeReadWrite} }
func (t *updateTool) DefaultPolicy() tool.ApprovalLevel {
	return tool.ApprovalAsk
}

func (t *updateTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name":        {"type": "string", "description": "Name of the cron to update (identifies the existing file)."},
			"description": {"type": "string", "description": "New description."},
			"schedule":    {"type": "string", "description": "New cron schedule."},
			"enabled":     {"type": "boolean", "description": "New enabled state."},
			"prompt":      {"type": "string", "description": "New prompt."},
			"tools":       {"type": "array", "items": {"type": "string"}, "description": "New tool filter."},
			"loop":        {"type": "object", "properties": {"max_iterations": {"type": "integer"}, "timeout": {"type": "string"}}, "description": "New loop config overrides."},
			"output":      {"type": "object", "properties": {"channel": {"type": "string"}, "chat_id": {"type": "string"}}, "description": "New output destination (null to remove)."}
		},
		"required": ["name"],
		"additionalProperties": false
	}`)
}

// updateArgs holds the raw JSON fields for selective update.
type updateArgs struct {
	Name        string                 `json:"name"`
	Description *string                `json:"description,omitempty"`
	Schedule    *string                `json:"schedule,omitempty"`
	Enabled     *bool                  `json:"enabled,omitempty"`
	Prompt      *string                `json:"prompt,omitempty"`
	Tools       *[]string              `json:"tools,omitempty"`
	Loop        *cron.PromptCronLoop   `json:"loop,omitempty"`
	Output      *cron.PromptCronOutput `json:"output,omitempty"`
}

func (t *updateTool) Execute(_ context.Context, args json.RawMessage, env tool.ExecutionEnv) (tool.Output, error) {
	var a updateArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Output{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	if !isValidCronName(a.Name) {
		return tool.Output{Content: fmt.Sprintf("invalid cron name: %q", a.Name), IsError: true}, nil
	}

	// Load existing definition.
	dir := cron.CronsDir(env.DataDir)
	path := filepath.Join(dir, a.Name+".json")
	def, err := cron.LoadPromptCronDef(path)
	if err != nil {
		return tool.Output{Content: fmt.Sprintf("cron %q not found or invalid: %v", a.Name, err), IsError: true}, nil
	}

	// Apply updates selectively.
	if a.Description != nil {
		def.Description = *a.Description
	}
	if a.Schedule != nil {
		def.Schedule = *a.Schedule
	}
	if a.Enabled != nil {
		def.Enabled = *a.Enabled
	}
	if a.Prompt != nil {
		def.Prompt = *a.Prompt
	}
	if a.Tools != nil {
		def.Tools = *a.Tools
	}
	if a.Loop != nil {
		def.Loop = *a.Loop
	}
	// Output can be explicitly set or removed (null removes it via omitempty).
	// We check if the raw JSON contains the "output" key.
	if rawHasKey(args, "output") {
		def.Output = a.Output
	}

	// Validate the updated definition.
	if err := def.Validate(); err != nil {
		return tool.Output{Content: fmt.Sprintf("validation error: %v", err), IsError: true}, nil
	}

	// Write updated definition.
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return tool.Output{Content: fmt.Sprintf("failed to marshal definition: %v", err), IsError: true}, nil
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return tool.Output{Content: fmt.Sprintf("failed to write file: %v", err), IsError: true}, nil
	}

	// Trigger reload.
	if t.deps.ReloadFn != nil {
		if err := t.deps.ReloadFn(); err != nil {
			return tool.Output{Content: fmt.Sprintf("cron updated but reload failed: %v", err), IsError: true}, nil
		}
	}

	return tool.Output{Content: fmt.Sprintf("prompt cron %q updated successfully", a.Name)}, nil
}

// rawHasKey checks if a JSON object contains a specific key at the top level.
func rawHasKey(raw json.RawMessage, key string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	_, ok := m[key]
	return ok
}
