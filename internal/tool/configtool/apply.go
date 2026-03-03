package configtool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flemzord/sclaw/internal/config"
	"github.com/flemzord/sclaw/internal/tool"
	"gopkg.in/yaml.v3"
)

type applyTool struct {
	deps Deps
}

func newApplyTool(deps Deps) tool.Tool {
	return &applyTool{deps: deps}
}

func (t *applyTool) Name() string { return "config_apply" }
func (t *applyTool) Description() string {
	return "Replace the entire configuration with new YAML content. Requires base_hash from config_get for concurrency control. Validates before writing and triggers a hot-reload."
}
func (t *applyTool) Scopes() []tool.Scope              { return []tool.Scope{tool.ScopeReadWrite} }
func (t *applyTool) DefaultPolicy() tool.ApprovalLevel { return tool.ApprovalAsk }

func (t *applyTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"base_hash": {
				"type": "string",
				"description": "SHA-256 hash from config_get. The apply is rejected if the file changed since this hash was computed."
			},
			"yaml_content": {
				"type": "string",
				"description": "Complete YAML configuration content to write."
			}
		},
		"required": ["base_hash", "yaml_content"]
	}`)
}

type applyArgs struct {
	BaseHash    string `json:"base_hash"`
	YAMLContent string `json:"yaml_content"`
}

func (t *applyTool) Execute(ctx context.Context, args json.RawMessage, _ tool.ExecutionEnv) (tool.Output, error) {
	var a applyArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Output{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	if a.BaseHash == "" || a.YAMLContent == "" {
		return tool.Output{Content: `{"status":"error","error":"base_hash and yaml_content are required"}`, IsError: true}, nil
	}

	newBytes := []byte(a.YAMLContent)
	if len(newBytes) > maxConfigSize {
		return tool.Output{Content: fmt.Sprintf(`{"status":"error","error":"content too large: %d bytes (max %d)"}`, len(newBytes), maxConfigSize), IsError: true}, nil
	}

	// Step 1: Read current file and verify hash.
	currentHash, rawBytes, err := fileHash(t.deps.ConfigPath)
	if err != nil {
		return errorOutput("failed to read config: " + err.Error()), nil
	}

	if currentHash != a.BaseHash {
		return errorOutput("hash mismatch: config was modified since last read (expected " + a.BaseHash + ", got " + currentHash + ")"), nil
	}

	// Step 2: Validate the new content (expand env + parse + Validate).
	cfg, err := config.LoadFromBytes(newBytes)
	if err != nil {
		return errorOutput("validation failed: " + err.Error()), nil
	}
	if err := config.Validate(cfg); err != nil {
		return errorOutput("validation failed: " + err.Error()), nil
	}

	// Step 3: Detect plugin changes (warning only).
	var oldCfg config.Config
	_ = yaml.Unmarshal(rawBytes, &oldCfg)

	var warnings []string
	if pluginsChanged(&oldCfg, cfg) {
		warnings = append(warnings, "plugin list changed — a rebuild may be required")
	}

	// Step 4: Atomic write.
	if err := atomicWrite(t.deps.ConfigPath, newBytes); err != nil {
		return errorOutput("failed to write config: " + err.Error()), nil
	}

	// Step 5: Reload modules.
	if t.deps.ReloadFn != nil {
		if err := t.deps.ReloadFn(ctx, t.deps.ConfigPath); err != nil {
			warnings = append(warnings, "reload failed: "+err.Error())
		}
	}

	out := patchOutput{
		Status:   "applied",
		NewHash:  bytesHash(newBytes),
		Warnings: warnings,
	}
	data, _ := json.Marshal(out)
	return tool.Output{Content: string(data)}, nil
}
