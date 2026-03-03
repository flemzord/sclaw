package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/flemzord/sclaw/internal/tool"
)

// maxFileSize is the maximum file size read_file will accept (1 MiB).
const maxFileSize = 1 << 20

type readFileTool struct{}

func (t *readFileTool) Name() string { return "read_file" }

func (t *readFileTool) Description() string {
	return "Read the contents of a file in the workspace, data directory, or allowed directories."
}

func (t *readFileTool) Scopes() []tool.Scope {
	return []tool.Scope{tool.ScopeReadOnly}
}

func (t *readFileTool) DefaultPolicy() tool.ApprovalLevel {
	return tool.ApprovalAllow
}

func (t *readFileTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "File path (relative to workspace, or absolute within workspace/data directory/allowed directories)."}
		},
		"required": ["path"]
	}`)
}

type readFileArgs struct {
	Path string `json:"path"`
}

func (t *readFileTool) Execute(_ context.Context, args json.RawMessage, env tool.ExecutionEnv) (tool.Output, error) {
	var a readFileArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Output{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	resolved, err := SafePathForRead(env.Workspace, env.DataDir, a.Path, env.PathFilter)
	if err != nil {
		return tool.Output{Content: fmt.Sprintf("path error: %v", err), IsError: true}, nil
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return tool.Output{Content: fmt.Sprintf("stat error: %v", err), IsError: true}, nil
	}
	if info.IsDir() {
		return tool.Output{Content: "path is a directory, not a file", IsError: true}, nil
	}
	if info.Size() > maxFileSize {
		return tool.Output{Content: fmt.Sprintf("file too large: %d bytes (max %d)", info.Size(), maxFileSize), IsError: true}, nil
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return tool.Output{Content: fmt.Sprintf("read error: %v", err), IsError: true}, nil
	}

	return tool.Output{Content: string(data)}, nil
}
