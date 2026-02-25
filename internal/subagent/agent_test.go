package subagent

import (
	"errors"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/provider"
)

func TestSubAgent_Snapshot(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	finished := now.Add(5 * time.Second)
	testErr := errors.New("something broke")
	testResult := &agent.Response{
		Content:    "done",
		Iterations: 3,
		StopReason: agent.StopReasonComplete,
	}

	sa := &SubAgent{
		ID:           "sa-001",
		ParentID:     "parent-001",
		Status:       StatusFailed,
		SystemPrompt: "You are a helper.",
		History: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "hello"},
			{Role: provider.MessageRoleAssistant, Content: "hi"},
		},
		Result:     testResult,
		Error:      testErr,
		CreatedAt:  now,
		FinishedAt: finished,
	}

	snap := sa.Snapshot()

	if snap.ID != "sa-001" {
		t.Errorf("ID = %q, want %q", snap.ID, "sa-001")
	}
	if snap.ParentID != "parent-001" {
		t.Errorf("ParentID = %q, want %q", snap.ParentID, "parent-001")
	}
	if snap.Status != StatusFailed {
		t.Errorf("Status = %q, want %q", snap.Status, StatusFailed)
	}
	if snap.SystemPrompt != "You are a helper." {
		t.Errorf("SystemPrompt = %q, want %q", snap.SystemPrompt, "You are a helper.")
	}
	if len(snap.History) != 2 {
		t.Fatalf("History len = %d, want 2", len(snap.History))
	}
	if snap.History[0].Content != "hello" {
		t.Errorf("History[0].Content = %q, want %q", snap.History[0].Content, "hello")
	}
	if snap.History[1].Content != "hi" {
		t.Errorf("History[1].Content = %q, want %q", snap.History[1].Content, "hi")
	}
	if snap.Result == nil {
		t.Fatal("Result is nil")
	}
	if snap.Result.Content != "done" {
		t.Errorf("Result.Content = %q, want %q", snap.Result.Content, "done")
	}
	if snap.Result.Iterations != 3 {
		t.Errorf("Result.Iterations = %d, want 3", snap.Result.Iterations)
	}
	if snap.ErrorMsg != "something broke" {
		t.Errorf("ErrorMsg = %q, want %q", snap.ErrorMsg, "something broke")
	}
	if !snap.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", snap.CreatedAt, now)
	}
	if !snap.FinishedAt.Equal(finished) {
		t.Errorf("FinishedAt = %v, want %v", snap.FinishedAt, finished)
	}
}

func TestSubAgent_Snapshot_IsolatedCopy(t *testing.T) {
	t.Parallel()

	sa := &SubAgent{
		ID:       "sa-002",
		ParentID: "parent-002",
		Status:   StatusRunning,
		History: []provider.LLMMessage{
			{Role: provider.MessageRoleUser, Content: "original"},
		},
		Result: &agent.Response{
			Content:    "initial result",
			Iterations: 1,
			StopReason: agent.StopReasonComplete,
		},
		CreatedAt: time.Now(),
	}

	snap := sa.Snapshot()

	// Modify the original after taking the snapshot.
	sa.mu.Lock()
	sa.Status = StatusCompleted
	sa.History = append(sa.History, provider.LLMMessage{
		Role:    provider.MessageRoleAssistant,
		Content: "new message",
	})
	sa.History[0].Content = "modified"
	sa.Result.Content = "modified result"
	sa.mu.Unlock()

	// Verify snapshot is unaffected.
	if snap.Status != StatusRunning {
		t.Errorf("snap.Status = %q, want %q (should not be modified)", snap.Status, StatusRunning)
	}
	if len(snap.History) != 1 {
		t.Fatalf("snap.History len = %d, want 1 (should not have new message)", len(snap.History))
	}
	if snap.History[0].Content != "original" {
		t.Errorf("snap.History[0].Content = %q, want %q (should not be modified)", snap.History[0].Content, "original")
	}
	if snap.Result.Content != "initial result" {
		t.Errorf("snap.Result.Content = %q, want %q (should not be modified)", snap.Result.Content, "initial result")
	}
}

func TestSubAgent_Snapshot_NilResult(t *testing.T) {
	t.Parallel()

	sa := &SubAgent{
		ID:        "sa-003",
		ParentID:  "parent-003",
		Status:    StatusRunning,
		CreatedAt: time.Now(),
	}

	snap := sa.Snapshot()

	if snap.Result != nil {
		t.Error("expected nil Result in snapshot when original has nil Result")
	}
	if snap.ErrorMsg != "" {
		t.Errorf("expected empty ErrorMsg, got %q", snap.ErrorMsg)
	}
}
