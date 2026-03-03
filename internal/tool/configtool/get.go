package configtool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flemzord/sclaw/internal/tool"
	"gopkg.in/yaml.v3"
)

type getTool struct {
	deps Deps
}

func newGetTool(deps Deps) tool.Tool {
	return &getTool{deps: deps}
}

func (t *getTool) Name() string { return "config_get" }
func (t *getTool) Description() string {
	return "Read the current sclaw configuration (redacted) and its base hash for use in subsequent patch/apply calls."
}
func (t *getTool) Scopes() []tool.Scope              { return []tool.Scope{tool.ScopeReadOnly} }
func (t *getTool) DefaultPolicy() tool.ApprovalLevel { return tool.ApprovalAllow }

func (t *getTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {},
		"additionalProperties": false
	}`)
}

type getOutput struct {
	Config   any    `json:"config"`
	BaseHash string `json:"base_hash"`
}

func (t *getTool) Execute(_ context.Context, _ json.RawMessage, _ tool.ExecutionEnv) (tool.Output, error) {
	hash, raw, err := fileHash(t.deps.ConfigPath)
	if err != nil {
		return tool.Output{Content: fmt.Sprintf("failed to read config: %v", err), IsError: true}, nil
	}

	if len(raw) > maxConfigSize {
		return tool.Output{Content: fmt.Sprintf("config file too large: %d bytes (max %d)", len(raw), maxConfigSize), IsError: true}, nil
	}

	// Parse YAML into a generic map for redaction.
	var configMap map[string]any
	if err := yaml.Unmarshal(raw, &configMap); err != nil {
		return tool.Output{Content: fmt.Sprintf("failed to parse config: %v", err), IsError: true}, nil
	}

	// Redact secrets before returning to the agent.
	if t.deps.Redactor != nil {
		t.deps.Redactor.RedactMap(configMap)
	}

	out := getOutput{
		Config:   configMap,
		BaseHash: hash,
	}

	data, err := json.Marshal(out)
	if err != nil {
		return tool.Output{Content: fmt.Sprintf("failed to marshal output: %v", err), IsError: true}, nil
	}

	return tool.Output{Content: string(data)}, nil
}
