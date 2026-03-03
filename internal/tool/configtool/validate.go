package configtool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flemzord/sclaw/internal/config"
	"github.com/flemzord/sclaw/internal/tool"
)

type validateTool struct{}

func newValidateTool() tool.Tool {
	return &validateTool{}
}

func (t *validateTool) Name() string { return "config.validate" }
func (t *validateTool) Description() string {
	return "Validate YAML configuration without writing to disk (dry-run). Returns validation errors if any."
}
func (t *validateTool) Scopes() []tool.Scope              { return []tool.Scope{tool.ScopeReadOnly} }
func (t *validateTool) DefaultPolicy() tool.ApprovalLevel { return tool.ApprovalAllow }

func (t *validateTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"yaml_content": {
				"type": "string",
				"description": "Complete YAML configuration content to validate."
			}
		},
		"required": ["yaml_content"]
	}`)
}

type validateArgs struct {
	YAMLContent string `json:"yaml_content"`
}

type validateOutput struct {
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

func (t *validateTool) Execute(_ context.Context, args json.RawMessage, _ tool.ExecutionEnv) (tool.Output, error) {
	var a validateArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Output{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	if a.YAMLContent == "" {
		return tool.Output{Content: `{"valid":false,"error":"yaml_content is required"}`, IsError: true}, nil
	}

	raw := []byte(a.YAMLContent)
	if len(raw) > maxConfigSize {
		return tool.Output{Content: fmt.Sprintf(`{"valid":false,"error":"content too large: %d bytes (max %d)"}`, len(raw), maxConfigSize), IsError: true}, nil
	}

	cfg, err := config.LoadFromBytes(raw)
	if err != nil {
		out := validateOutput{Valid: false, Error: err.Error()}
		data, _ := json.Marshal(out)
		return tool.Output{Content: string(data)}, nil
	}

	if err := config.Validate(cfg); err != nil {
		out := validateOutput{Valid: false, Error: err.Error()}
		data, _ := json.Marshal(out)
		return tool.Output{Content: string(data)}, nil
	}

	out := validateOutput{Valid: true}
	data, _ := json.Marshal(out)
	return tool.Output{Content: string(data)}, nil
}
