package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/flemzord/sclaw/internal/tool"
)

// maxWriteSize is the maximum content size write_file will accept (1 MiB).
const maxWriteSize = 1 << 20

type writeFileTool struct{}

func (t *writeFileTool) Name() string { return "write_file" }

func (t *writeFileTool) Description() string {
	return "Write content to a file in the workspace or read-write allowed directories."
}

func (t *writeFileTool) Scopes() []tool.Scope {
	return []tool.Scope{tool.ScopeReadWrite}
}

func (t *writeFileTool) DefaultPolicy() tool.ApprovalLevel {
	return tool.ApprovalAllow
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

	if len(a.Content) > maxWriteSize {
		return tool.Output{
			Content: fmt.Sprintf("content too large: %d bytes (max %d)", len(a.Content), maxWriteSize),
			IsError: true,
		}, nil
	}

	resolved, err := safePathForWriteFiltered(env.Workspace, a.Path, env.PathFilter)
	if err != nil {
		return tool.Output{Content: fmt.Sprintf("path error: %v", err), IsError: true}, nil
	}

	if err := os.WriteFile(resolved, []byte(a.Content), 0o644); err != nil {
		return tool.Output{Content: fmt.Sprintf("write error: %v", err), IsError: true}, nil
	}

	return tool.Output{Content: fmt.Sprintf("wrote %d bytes to %s", len(a.Content), a.Path)}, nil
}
