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

type deleteTool struct {
	deps Deps
}

func newDeleteTool(deps Deps) tool.Tool { return &deleteTool{deps: deps} }

func (t *deleteTool) Name() string { return "cron_delete" }
func (t *deleteTool) Description() string {
	return "Delete a prompt cron definition and its last result."
}
func (t *deleteTool) Scopes() []tool.Scope { return []tool.Scope{tool.ScopeReadWrite} }
func (t *deleteTool) DefaultPolicy() tool.ApprovalLevel {
	return tool.ApprovalAsk
}

func (t *deleteTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Name of the prompt cron to delete."}
		},
		"required": ["name"],
		"additionalProperties": false
	}`)
}

type deleteArgs struct {
	Name string `json:"name"`
}

func (t *deleteTool) Execute(_ context.Context, args json.RawMessage, env tool.ExecutionEnv) (tool.Output, error) {
	var a deleteArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Output{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	if !isValidCronName(a.Name) {
		return tool.Output{Content: fmt.Sprintf("invalid cron name: %q", a.Name), IsError: true}, nil
	}

	// Verify the cron exists.
	dir := cron.CronsDir(env.DataDir)
	defPath := filepath.Join(dir, a.Name+".json")
	if _, err := os.Stat(defPath); os.IsNotExist(err) {
		return tool.Output{Content: fmt.Sprintf("cron %q not found", a.Name), IsError: true}, nil
	}

	// Remove the definition file.
	if err := os.Remove(defPath); err != nil {
		return tool.Output{Content: fmt.Sprintf("failed to delete cron %q: %v", a.Name, err), IsError: true}, nil
	}

	// Also remove the result file if it exists.
	resultPath := filepath.Join(cron.ResultsDir(env.DataDir), a.Name+".json")
	_ = os.Remove(resultPath) // best-effort

	// Trigger reload.
	if t.deps.ReloadFn != nil {
		if err := t.deps.ReloadFn(); err != nil {
			return tool.Output{Content: fmt.Sprintf("cron deleted but reload failed: %v", err), IsError: true}, nil
		}
	}

	return tool.Output{Content: fmt.Sprintf("prompt cron %q deleted successfully", a.Name)}, nil
}
