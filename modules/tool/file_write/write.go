package filewrite

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/flemzord/sclaw/internal/tool"
	"github.com/flemzord/sclaw/internal/tool/safepath"
)

type writeFileTool struct {
	maxFileSize int
	createDirs  bool
	policy      tool.ApprovalLevel
}

func (t *writeFileTool) Name() string { return "write_file" }

func (t *writeFileTool) Description() string {
	return "Write content to a file in the workspace or read-write allowed directories."
}

func (t *writeFileTool) Scopes() []tool.Scope {
	return []tool.Scope{tool.ScopeReadWrite}
}

func (t *writeFileTool) DefaultPolicy() tool.ApprovalLevel {
	return t.policy
}

func (t *writeFileTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "File path (relative to workspace, or absolute within workspace/read-write allowed directories)."},
			"content": {"type": "string", "description": "Content to write to the file."}
		},
		"required": ["path", "content"]
	}`)
}

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *writeFileTool) Execute(_ context.Context, args json.RawMessage, env tool.ExecutionEnv) (tool.Output, error) {
	var a writeFileArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Output{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	if len(a.Content) > t.maxFileSize {
		return tool.Output{
			Content: fmt.Sprintf("content too large: %d bytes (max %d)", len(a.Content), t.maxFileSize),
			IsError: true,
		}, nil
	}

	resolved, err := safepath.ForWrite(env.Workspace, a.Path, env.PathFilter)
	if err != nil {
		return tool.Output{Content: fmt.Sprintf("path error: %v", err), IsError: true}, nil
	}

	// Create parent directories if configured.
	if t.createDirs {
		if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
			return tool.Output{Content: fmt.Sprintf("mkdir error: %v", err), IsError: true}, nil
		}
	}

	if err := os.WriteFile(resolved, []byte(a.Content), 0o644); err != nil {
		return tool.Output{Content: fmt.Sprintf("write error: %v", err), IsError: true}, nil
	}

	return tool.Output{Content: fmt.Sprintf("wrote %d bytes to %s", len(a.Content), a.Path)}, nil
}
