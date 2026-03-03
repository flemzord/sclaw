package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/flemzord/sclaw/internal/tool"
)

const (
	// defaultExecTimeout is the default timeout for exec commands.
	defaultExecTimeout = 30 * time.Second

	// maxExecTimeout is the maximum allowed timeout for exec commands.
	maxExecTimeout = 10 * time.Minute

	// maxOutputSize is the maximum size of stdout+stderr captured (1 MiB).
	maxOutputSize = 1 << 20
)

type execTool struct{}

func (t *execTool) Name() string        { return "exec" }
func (t *execTool) Description() string { return "Execute a shell command in the workspace." }
func (t *execTool) Scopes() []tool.Scope {
	return []tool.Scope{tool.ScopeExec}
}

func (t *execTool) DefaultPolicy() tool.ApprovalLevel {
	return tool.ApprovalAllow
}

func (t *execTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "Shell command to execute (passed to sh -c)."},
			"timeout_seconds": {"type": "integer", "description": "Optional timeout in seconds (default 30, max 600)."}
		},
		"required": ["command"]
	}`)
}

type execArgs struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

func (t *execTool) Execute(ctx context.Context, args json.RawMessage, env tool.ExecutionEnv) (tool.Output, error) {
	var a execArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return tool.Output{Content: fmt.Sprintf("invalid arguments: %v", err), IsError: true}, nil
	}

	if a.Command == "" {
		return tool.Output{Content: "command is empty", IsError: true}, nil
	}

	timeout := defaultExecTimeout
	if a.TimeoutSeconds > 0 {
		timeout = time.Duration(a.TimeoutSeconds) * time.Second
		if timeout > maxExecTimeout {
			timeout = maxExecTimeout
		}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", a.Command)
	cmd.Dir = env.Workspace
	if env.SanitizedEnv != nil {
		cmd.Env = env.SanitizedEnv
	}

	stdout := &limitedWriter{max: maxOutputSize}
	stderr := &limitedWriter{max: maxOutputSize}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()

	var output string
	if stdout.Len() > 0 {
		output = stdout.String()
	}
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	if err != nil {
		if output == "" {
			output = err.Error()
		}
		return tool.Output{Content: output, IsError: true}, nil
	}

	return tool.Output{Content: output}, nil
}

// limitedWriter is a bytes.Buffer that stops accepting writes after max bytes.
type limitedWriter struct {
	buf bytes.Buffer
	max int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	remaining := w.max - w.buf.Len()
	if remaining <= 0 {
		return len(p), nil // Discard silently.
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	return w.buf.Write(p)
}

func (w *limitedWriter) String() string { return w.buf.String() }
func (w *limitedWriter) Len() int       { return w.buf.Len() }
