package configtool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/flemzord/sclaw/internal/config"
	"github.com/flemzord/sclaw/internal/tool"
	"gopkg.in/yaml.v3"
)

type patchTool struct {
	deps Deps
}

func newPatchTool(deps Deps) tool.Tool {
	return &patchTool{deps: deps}
}

func (t *patchTool) Name() string { return "config.patch" }
func (t *patchTool) Description() string {
	return "Partially merge a YAML patch into the current configuration. Requires base_hash from config.get for concurrency control. Validates before writing and triggers a hot-reload."
}
func (t *patchTool) Scopes() []tool.Scope              { return []tool.Scope{tool.ScopeReadWrite} }
func (t *patchTool) DefaultPolicy() tool.ApprovalLevel { return tool.ApprovalAsk }

func (t *patchTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"base_hash": {
				"type": "string",
				"description": "SHA-256 hash from config.get. The patch is rejected if the file changed since this hash was computed."
			},
			"patch": {
				"type": "string",
				"description": "YAML content to merge into the current configuration (RFC 7386 merge patch semantics)."
			}
		},
		"required": ["base_hash", "patch"]
	}`)
}

type patchArgs struct {
	BaseHash string `json:"base_hash"`
	Patch    string `json:"patch"`
}

type patchOutput struct {
	Status   string   `json:"status"`
	NewHash  string   `json:"new_hash,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Error    string   `json:"error,omitempty"`
}

func (t *patchTool) Execute(ctx context.Context, args json.RawMessage, _ tool.ExecutionEnv) (tool.Output, error) {
	var a patchArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Output{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	if a.BaseHash == "" || a.Patch == "" {
		return tool.Output{Content: `{"status":"error","error":"base_hash and patch are required"}`, IsError: true}, nil
	}

	patchBytes := []byte(a.Patch)
	if len(patchBytes) > maxConfigSize {
		return tool.Output{Content: fmt.Sprintf(`{"status":"error","error":"patch too large: %d bytes (max %d)"}`, len(patchBytes), maxConfigSize), IsError: true}, nil
	}

	// Step 1: Read current file and verify hash.
	currentHash, rawBytes, err := fileHash(t.deps.ConfigPath)
	if err != nil {
		return errorOutput("failed to read config: " + err.Error()), nil
	}

	if currentHash != a.BaseHash {
		return errorOutput("hash mismatch: config was modified since last read (expected " + a.BaseHash + ", got " + currentHash + ")"), nil
	}

	// Step 2: Parse base and patch as yaml.Node trees.
	var baseNode yaml.Node
	if err := yaml.Unmarshal(rawBytes, &baseNode); err != nil {
		return errorOutput("failed to parse current config: " + err.Error()), nil
	}

	var patchNode yaml.Node
	if err := yaml.Unmarshal(patchBytes, &patchNode); err != nil {
		return errorOutput("failed to parse patch: " + err.Error()), nil
	}

	// Step 3: Detect plugin changes before merge (for warning).
	var oldCfg config.Config
	_ = yaml.Unmarshal(rawBytes, &oldCfg)

	// Step 4: Merge patch into base.
	merged := mergeNodes(&baseNode, &patchNode)

	// Step 5: Marshal merged result.
	mergedBytes, err := yaml.Marshal(merged)
	if err != nil {
		return errorOutput("failed to marshal merged config: " + err.Error()), nil
	}

	if len(mergedBytes) > maxConfigSize {
		return errorOutput(fmt.Sprintf("merged config too large: %d bytes (max %d)", len(mergedBytes), maxConfigSize)), nil
	}

	// Step 6: Validate the merged config (expand env + parse + Validate).
	cfg, err := config.LoadFromBytes(mergedBytes)
	if err != nil {
		return errorOutput("validation failed: " + err.Error()), nil
	}
	if err := config.Validate(cfg); err != nil {
		return errorOutput("validation failed: " + err.Error()), nil
	}

	// Step 7: Detect plugin changes (warning only).
	var warnings []string
	if pluginsChanged(&oldCfg, cfg) {
		warnings = append(warnings, "plugin list changed — a rebuild may be required")
	}

	// Step 8: Atomic write.
	if err := atomicWrite(t.deps.ConfigPath, mergedBytes); err != nil {
		return errorOutput("failed to write config: " + err.Error()), nil
	}

	// Step 9: Reload modules.
	if t.deps.ReloadFn != nil {
		if err := t.deps.ReloadFn(ctx, t.deps.ConfigPath); err != nil {
			warnings = append(warnings, "reload failed: "+err.Error())
		}
	}

	out := patchOutput{
		Status:   "applied",
		NewHash:  bytesHash(mergedBytes),
		Warnings: warnings,
	}
	data, _ := json.Marshal(out)
	return tool.Output{Content: string(data)}, nil
}

// atomicWrite writes data to path atomically using a temporary file + rename.
func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		// Cleanup temp file on rename failure.
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

// pluginsChanged compares plugin lists between old and new configs.
func pluginsChanged(old, updated *config.Config) bool {
	if len(old.Plugins) != len(updated.Plugins) {
		return true
	}
	for i := range old.Plugins {
		if old.Plugins[i] != updated.Plugins[i] {
			return true
		}
	}
	return false
}

func errorOutput(msg string) tool.Output {
	out := patchOutput{Status: "error", Error: msg}
	data, _ := json.Marshal(out)
	return tool.Output{Content: string(data), IsError: true}
}
