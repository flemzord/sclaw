package router

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/flemzord/sclaw/internal/tool"
	"github.com/flemzord/sclaw/pkg/message"
)

type approvalRawPayload struct {
	ApprovalID string `json:"approval_id"`
	Approved   *bool  `json:"approved"`
	Reason     string `json:"reason,omitempty"`
}

// pendingEntry tracks a pending tool approval and its session.
type pendingEntry struct {
	approval   *tool.PendingApproval
	sessionKey SessionKey
}

// ApprovalManager handles tool approval responses that bypass the lane lock.
type ApprovalManager struct {
	mu      sync.Mutex
	pending map[string]*pendingEntry
}

// NewApprovalManager creates a new ApprovalManager.
func NewApprovalManager() *ApprovalManager {
	return &ApprovalManager{
		pending: make(map[string]*pendingEntry),
	}
}

// Register stores a pending approval for later resolution.
func (am *ApprovalManager) Register(id string, approval *tool.PendingApproval, sessionKey SessionKey) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.pending[id] = &pendingEntry{
		approval:   approval,
		sessionKey: sessionKey,
	}
}

// Resolve sends an approval response to the pending approval flow.
// Returns true if the approval was found and resolved.
// This bypasses the lane lock entirely — approval responses go directly to the agent.
func (am *ApprovalManager) Resolve(id string, response tool.ApprovalResponse) bool {
	am.mu.Lock()
	entry, ok := am.pending[id]
	if ok {
		delete(am.pending, id)
	}
	am.mu.Unlock()

	if !ok {
		return false
	}

	return entry.approval.Respond(response)
}

// Remove cleans up a pending approval entry (e.g., after timeout).
func (am *ApprovalManager) Remove(id string) {
	am.mu.Lock()
	defer am.mu.Unlock()
	delete(am.pending, id)
}

// IsApprovalResponse checks if an inbound message is an approval response.
// Returns the approval ID, the response, and whether it matched.
// Currently checks message metadata for approval_id and approved fields.
func (am *ApprovalManager) IsApprovalResponse(msg message.InboundMessage) (string, tool.ApprovalResponse, bool) {
	if id, resp, ok := approvalResponseFromRaw(msg.Raw); ok {
		return id, resp, true
	}
	return approvalResponseFromText(msg.TextContent())
}

func approvalResponseFromRaw(raw json.RawMessage) (string, tool.ApprovalResponse, bool) {
	if len(raw) == 0 {
		return "", tool.ApprovalResponse{}, false
	}

	var payload approvalRawPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", tool.ApprovalResponse{}, false
	}
	if payload.ApprovalID == "" || payload.Approved == nil {
		return "", tool.ApprovalResponse{}, false
	}

	return payload.ApprovalID, tool.ApprovalResponse{
		Approved: *payload.Approved,
		Reason:   payload.Reason,
	}, true
}

func approvalResponseFromText(text string) (string, tool.ApprovalResponse, bool) {
	parts := strings.Fields(strings.TrimSpace(text))
	if len(parts) < 2 {
		return "", tool.ApprovalResponse{}, false
	}

	switch strings.ToLower(parts[0]) {
	case "approve":
		return parts[1], tool.ApprovalResponse{Approved: true}, true
	case "deny", "reject":
		reason := ""
		if len(parts) > 2 {
			reason = strings.Join(parts[2:], " ")
		}
		return parts[1], tool.ApprovalResponse{
			Approved: false,
			Reason:   reason,
		}, true
	default:
		return "", tool.ApprovalResponse{}, false
	}
}
