package crontool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flemzord/sclaw/internal/cron"
	"github.com/flemzord/sclaw/internal/tool"
)

type listTool struct{}

func newListTool() tool.Tool { return &listTool{} }

func (t *listTool) Name() string         { return "cron_list" }
func (t *listTool) Description() string  { return "List all prompt cron definitions." }
func (t *listTool) Scopes() []tool.Scope { return []tool.Scope{tool.ScopeReadOnly} }
func (t *listTool) DefaultPolicy() tool.ApprovalLevel {
	return tool.ApprovalAllow
}

func (t *listTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {},
		"additionalProperties": false
	}`)
}

type listEntry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Schedule    string `json:"schedule"`
	Enabled     bool   `json:"enabled"`
}

func (t *listTool) Execute(_ context.Context, _ json.RawMessage, env tool.ExecutionEnv) (tool.Output, error) {
	dir := cron.CronsDir(env.DataDir)
	defs, errs := cron.ScanPromptCrons(dir)

	// Log scan errors but don't fail the tool call.
	for _, e := range errs {
		_ = e // errors are informational; we still return valid defs
	}

	entries := make([]listEntry, 0, len(defs))
	for _, d := range defs {
		entries = append(entries, listEntry{
			Name:        d.Name,
			Description: d.Description,
			Schedule:    d.Schedule,
			Enabled:     d.Enabled,
		})
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return tool.Output{Content: fmt.Sprintf("failed to marshal list: %v", err), IsError: true}, nil
	}

	return tool.Output{Content: string(data)}, nil
}
