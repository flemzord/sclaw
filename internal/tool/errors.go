package tool

import "errors"

var (
	// ErrToolNotFound is returned when a tool is not found in the registry.
	ErrToolNotFound = errors.New("tool not found")

	// ErrDenied is returned when a tool execution is denied by policy.
	ErrDenied = errors.New("tool execution denied by policy")

	// ErrApprovalTimeout is returned when an approval request times out.
	ErrApprovalTimeout = errors.New("approval request timed out")

	// ErrNoScopes is returned when a tool declares no scopes.
	ErrNoScopes = errors.New("tool must declare at least one scope")

	// ErrEmptyToolName is returned when a tool name is empty.
	ErrEmptyToolName = errors.New("tool name must not be empty")

	// ErrDuplicateTool is returned when registering a tool with a name that
	// already exists in the registry.
	ErrDuplicateTool = errors.New("tool already registered")

	// ErrToolInMultipleLists is returned when a tool appears in conflicting
	// policy lists (e.g., both allow and deny).
	ErrToolInMultipleLists = errors.New("tool appears in conflicting policy lists")
)
