package tool

import (
	"encoding/json"
	"testing"
)

func TestApprovalRequest_Fields(t *testing.T) {
	t.Parallel()

	req := ApprovalRequest{
		ID:          "req-1",
		ToolName:    "exec_cmd",
		Description: "Execute a shell command",
		Arguments:   json.RawMessage(`{"cmd":"ls"}`),
		Context:     PolicyContextDM,
	}

	if req.ID != "req-1" {
		t.Errorf("ID: got %q, want %q", req.ID, "req-1")
	}
	if req.ToolName != "exec_cmd" {
		t.Errorf("ToolName: got %q, want %q", req.ToolName, "exec_cmd")
	}
	if req.Context != PolicyContextDM {
		t.Errorf("Context: got %q, want %q", req.Context, PolicyContextDM)
	}
}

func TestApprovalResponse_Approved(t *testing.T) {
	t.Parallel()

	resp := ApprovalResponse{Approved: true, Reason: "user clicked yes"}
	if !resp.Approved {
		t.Error("expected Approved to be true")
	}

	denied := ApprovalResponse{Approved: false, Reason: "too dangerous"}
	if denied.Approved {
		t.Error("expected Approved to be false")
	}
	if denied.Reason != "too dangerous" {
		t.Errorf("Reason: got %q, want %q", denied.Reason, "too dangerous")
	}
}
