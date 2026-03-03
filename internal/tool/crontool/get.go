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

type getTool struct{}

func newGetTool() tool.Tool { return &getTool{} }

func (t *getTool) Name() string { return "cron_get" }
func (t *getTool) Description() string {
	return "Get a prompt cron definition and its last run result."
}
func (t *getTool) Scopes() []tool.Scope { return []tool.Scope{tool.ScopeReadOnly} }
func (t *getTool) DefaultPolicy() tool.ApprovalLevel {
	return tool.ApprovalAllow
}

func (t *getTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Name of the prompt cron to retrieve."}
		},
		"required": ["name"],
		"additionalProperties": false
	}`)
}

type getArgs struct {
	Name string `json:"name"`
}

type getOutput struct {
	Definition cron.PromptCronDef     `json:"definition"`
	LastResult *cron.PromptCronResult `json:"last_result,omitempty"`
}

func (t *getTool) Execute(_ context.Context, args json.RawMessage, env tool.ExecutionEnv) (tool.Output, error) {
	var a getArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Output{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	if !isValidCronName(a.Name) {
		return tool.Output{Content: fmt.Sprintf("invalid cron name: %q", a.Name), IsError: true}, nil
	}

	// Load definition.
	defPath := filepath.Join(cron.CronsDir(env.DataDir), a.Name+".json")
	def, err := cron.LoadPromptCronDef(defPath)
	if err != nil {
		return tool.Output{Content: fmt.Sprintf("cron %q not found or invalid: %v", a.Name, err), IsError: true}, nil
	}

	out := getOutput{Definition: *def}

	// Try to load last result.
	resultPath := filepath.Join(cron.ResultsDir(env.DataDir), a.Name+".json")
	if resultData, err := os.ReadFile(resultPath); err == nil {
		var result cron.PromptCronResult
		if err := json.Unmarshal(resultData, &result); err == nil {
			out.LastResult = &result
		}
	}

	data, err := json.Marshal(out)
	if err != nil {
		return tool.Output{Content: fmt.Sprintf("failed to marshal output: %v", err), IsError: true}, nil
	}

	return tool.Output{Content: string(data)}, nil
}
