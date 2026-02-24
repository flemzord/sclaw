// Package tool defines the tool interface, execution model, and approval system
// for sclaw. Tools are the primary security boundary: every action an agent takes
// goes through a registered tool with policy-based approval.
package tool

import (
	"context"
	"encoding/json"
)

// Scope declares what kind of access a tool requires.
// Every tool must declare at least one scope.
type Scope string

// Scope values for tool access requirements.
const (
	ScopeReadOnly  Scope = "read_only"
	ScopeReadWrite Scope = "read_write"
	ScopeExec      Scope = "exec"
	ScopeNetwork   Scope = "network"
)

// Tool is the interface that all sclaw tools must implement.
// Tools are the fundamental unit of agent capability.
type Tool interface {
	// Name returns the unique identifier for this tool.
	Name() string

	// Description returns a human-readable description of what the tool does.
	Description() string

	// Schema returns a JSON Schema describing the tool's parameters.
	Schema() json.RawMessage

	// Scopes returns the access scopes this tool requires.
	// Must return at least one scope.
	Scopes() []Scope

	// DefaultPolicy returns the default approval level for this tool
	// when no explicit policy is configured.
	DefaultPolicy() ApprovalLevel

	// Execute runs the tool with the given arguments and environment.
	Execute(ctx context.Context, args json.RawMessage, env ExecutionEnv) (Output, error)
}

// ExecutionEnv provides the runtime environment for tool execution.
// It intentionally does not expose secrets or os.Environ.
type ExecutionEnv struct {
	// Workspace is the root directory for the current session.
	Workspace string

	// DataDir is the persistent data directory for the tool.
	DataDir string
}

// Output is the result of a tool execution.
type Output struct {
	// Content is the output text from the tool.
	Content string

	// IsError indicates whether the output represents an error condition.
	IsError bool
}
