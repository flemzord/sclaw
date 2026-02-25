package router

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/tool"
	"github.com/flemzord/sclaw/pkg/message"
)

func TestApprovalManager_RegisterResolve(t *testing.T) {
	t.Parallel()

	am := NewApprovalManager()
	pending := tool.NewPendingApproval()
	key := SessionKey{Channel: "slack", ChatID: "C1", ThreadID: "T1"}

	am.Register("approval-1", pending, key)

	// Resolve the approval.
	resp := tool.ApprovalResponse{Approved: true, Reason: "looks good"}
	ok := am.Resolve("approval-1", resp)
	if !ok {
		t.Fatal("expected Resolve to return true for registered approval")
	}

	// Verify the response was sent to the channel.
	select {
	case got := <-pending.ResponseChan:
		if got.Approved != true {
			t.Errorf("Approved = %v, want true", got.Approved)
		}
		if got.Reason != "looks good" {
			t.Errorf("Reason = %q, want %q", got.Reason, "looks good")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for approval response on channel")
	}
}

func TestApprovalManager_ResolveUnknown(t *testing.T) {
	t.Parallel()

	am := NewApprovalManager()

	// Resolve a non-existent approval.
	ok := am.Resolve("unknown-id", tool.ApprovalResponse{Approved: true})
	if ok {
		t.Error("expected Resolve to return false for unknown approval ID")
	}
}

func TestApprovalManager_DoubleResolve(t *testing.T) {
	t.Parallel()

	am := NewApprovalManager()
	pending := tool.NewPendingApproval()
	key := SessionKey{Channel: "slack", ChatID: "C1", ThreadID: "T1"}

	am.Register("approval-1", pending, key)

	// First resolve succeeds.
	ok := am.Resolve("approval-1", tool.ApprovalResponse{Approved: true})
	if !ok {
		t.Fatal("expected first Resolve to succeed")
	}

	// Second resolve for the same ID fails (already removed).
	ok = am.Resolve("approval-1", tool.ApprovalResponse{Approved: false})
	if ok {
		t.Error("expected second Resolve to return false (already resolved)")
	}
}

func TestApprovalManager_Remove(t *testing.T) {
	t.Parallel()

	am := NewApprovalManager()
	pending := tool.NewPendingApproval()
	key := SessionKey{Channel: "slack", ChatID: "C1", ThreadID: "T1"}

	am.Register("approval-1", pending, key)
	am.Remove("approval-1")

	// After removal, resolve should fail.
	ok := am.Resolve("approval-1", tool.ApprovalResponse{Approved: true})
	if ok {
		t.Error("expected Resolve to return false after Remove")
	}
}

func TestApprovalManager_IsApprovalResponse_FromRaw(t *testing.T) {
	t.Parallel()

	am := NewApprovalManager()
	approved := true
	msg := message.InboundMessage{
		ID:      "msg-1",
		Channel: "slack",
		Chat: message.Chat{
			ID:   "C123",
			Type: message.ChatDM,
		},
		Sender: message.Sender{
			ID:       "U001",
			Username: "testuser",
		},
		Blocks: []message.ContentBlock{
			message.NewTextBlock("approve"),
		},
		Raw: json.RawMessage(`{"approval_id":"approval-123","approved":true,"reason":"looks safe"}`),
	}

	id, resp, matched := am.IsApprovalResponse(msg)
	if !matched {
		t.Fatal("expected IsApprovalResponse to match raw payload")
	}
	if id != "approval-123" {
		t.Errorf("approval ID = %q, want %q", id, "approval-123")
	}
	if resp.Approved != approved {
		t.Errorf("Approved = %v, want %v", resp.Approved, approved)
	}
	if resp.Reason != "looks safe" {
		t.Errorf("Reason = %q, want %q", resp.Reason, "looks safe")
	}
}

func TestApprovalManager_IsApprovalResponse_FromText(t *testing.T) {
	t.Parallel()

	am := NewApprovalManager()
	msg := message.InboundMessage{
		Blocks: []message.ContentBlock{
			message.NewTextBlock("deny approval-abc risky command"),
		},
	}

	id, resp, matched := am.IsApprovalResponse(msg)
	if !matched {
		t.Fatal("expected IsApprovalResponse to match text command")
	}
	if id != "approval-abc" {
		t.Errorf("approval ID = %q, want %q", id, "approval-abc")
	}
	if resp.Approved {
		t.Error("Approved should be false for deny command")
	}
	if resp.Reason != "risky command" {
		t.Errorf("Reason = %q, want %q", resp.Reason, "risky command")
	}
}

func TestApprovalManager_IsApprovalResponse_Invalid(t *testing.T) {
	t.Parallel()

	am := NewApprovalManager()
	msg := message.InboundMessage{
		Blocks: []message.ContentBlock{
			message.NewTextBlock("hello there"),
		},
		Raw: json.RawMessage(`{"approval_id":"missing-approved"}`),
	}

	_, _, matched := am.IsApprovalResponse(msg)
	if matched {
		t.Error("expected IsApprovalResponse to return false for invalid payload")
	}
}
