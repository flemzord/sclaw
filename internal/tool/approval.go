package tool

import (
	"context"
	"encoding/json"
)

// ApprovalRequest is sent to an ApprovalRequester when a tool needs user confirmation.
type ApprovalRequest struct {
	// ID is a unique identifier for this approval request.
	ID string

	// ToolName is the name of the tool requesting approval.
	ToolName string

	// Description is a human-readable summary of what the tool will do.
	Description string

	// Arguments are the raw JSON arguments that will be passed to the tool.
	Arguments json.RawMessage

	// Context is the policy context (dm or group) where the request originates.
	Context PolicyContext
}

// ApprovalResponse is the result of an approval request.
type ApprovalResponse struct {
	// Approved indicates whether the user approved the tool execution.
	Approved bool

	// Reason is an optional explanation for the decision.
	Reason string
}

// ApprovalRequester handles requesting approval from a user.
// Implementations can provide different UX per channel type (inline buttons,
// reactions, etc.) via interface polymorphism.
type ApprovalRequester interface {
	// RequestApproval sends an approval request and blocks until a response
	// is received or the context is cancelled.
	RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalResponse, error)
}
