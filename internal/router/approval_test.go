package router

import (
	"context"
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

	type beginResult struct {
		resp tool.ApprovalResponse
		err  error
	}
	done := make(chan beginResult, 1)
	go func() {
		resp, err := pending.Begin(context.Background(), nil, tool.ApprovalRequest{
			ID:       "approval-1",
			ToolName: "read_file",
		}, time.Second)
		done <- beginResult{resp: resp, err: err}
	}()

	deadline := time.Now().Add(time.Second)
	for pending.State() != tool.StatePending {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for pending approval state")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Resolve the approval.
	resp := tool.ApprovalResponse{Approved: true, Reason: "looks good"}
	ok := am.Resolve("approval-1", resp)
	if !ok {
		t.Fatal("expected Resolve to return true for registered approval")
	}

	// Verify the pending flow received the response.
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("pending begin returned error: %v", got.err)
		}
		if got.resp.Approved != true {
			t.Errorf("Approved = %v, want true", got.resp.Approved)
		}
		if got.resp.Reason != "looks good" {
			t.Errorf("Reason = %q, want %q", got.resp.Reason, "looks good")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for approval response")
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

	type beginResult struct {
		resp tool.ApprovalResponse
		err  error
	}
	done := make(chan beginResult, 1)
	go func() {
		resp, err := pending.Begin(context.Background(), nil, tool.ApprovalRequest{
			ID:       "approval-1",
			ToolName: "read_file",
		}, time.Second)
		done <- beginResult{resp: resp, err: err}
	}()

	deadline := time.Now().Add(time.Second)
	for pending.State() != tool.StatePending {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for pending approval state")
		}
		time.Sleep(5 * time.Millisecond)
	}

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

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("pending begin returned error: %v", got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pending approval result")
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
