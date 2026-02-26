package subagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/tool"
)

func TestRegisterTools_FullAccess(t *testing.T) {
	t.Parallel()

	factory := &mockLoopFactory{
		resp: provider.CompletionResponse{
			Content:      "done",
			FinishReason: provider.FinishReasonStop,
		},
	}
	mgr := newTestManager(factory)
	reg := tool.NewRegistry()

	err := RegisterTools(reg, mgr, "parent-1", "", false)
	if err != nil {
		t.Fatalf("RegisterTools returned error: %v", err)
	}

	names := reg.Names()
	if len(names) != 5 {
		t.Fatalf("expected 5 tools registered, got %d: %v", len(names), names)
	}

	expected := []string{
		"sessions_history",
		"sessions_kill",
		"sessions_list",
		"sessions_send",
		"sessions_spawn",
	}
	for i, name := range expected {
		if names[i] != name {
			t.Errorf("names[%d] = %q, want %q", i, names[i], name)
		}
	}
}

func TestRegisterTools_SubAgentRestricted(t *testing.T) {
	t.Parallel()

	factory := &mockLoopFactory{}
	mgr := newTestManager(factory)
	reg := tool.NewRegistry()

	err := RegisterTools(reg, mgr, "parent-1", "", true)
	if err != nil {
		t.Fatalf("RegisterTools returned error: %v", err)
	}

	names := reg.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2 tools registered for sub-agent, got %d: %v", len(names), names)
	}

	expected := []string{
		"sessions_history",
		"sessions_list",
	}
	for i, name := range expected {
		if names[i] != name {
			t.Errorf("names[%d] = %q, want %q", i, names[i], name)
		}
	}
}

func TestSessionsSpawnTool_Execute(t *testing.T) {
	t.Parallel()

	factory := &mockLoopFactory{
		resp: provider.CompletionResponse{
			Content:      "sub done",
			FinishReason: provider.FinishReasonStop,
		},
	}
	mgr := newTestManager(factory)

	spawnTool := newSessionsSpawnTool(mgr, "parent-1", "")

	args, _ := json.Marshal(spawnArgs{
		SystemPrompt:   "You are a code reviewer.",
		InitialMessage: "Review this code.",
	})

	out, err := spawnTool.Execute(context.Background(), args, tool.ExecutionEnv{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if out.IsError {
		t.Fatalf("Execute returned error output: %s", out.Content)
	}

	var result map[string]string
	if unmarshalErr := json.Unmarshal([]byte(out.Content), &result); unmarshalErr != nil {
		t.Fatalf("failed to unmarshal output: %v", unmarshalErr)
	}
	if result["id"] == "" {
		t.Error("expected non-empty id in result")
	}
	if result["status"] != "running" {
		t.Errorf("status = %q, want %q", result["status"], "running")
	}
}

func TestSessionsSpawnTool_Execute_InvalidArgs(t *testing.T) {
	t.Parallel()

	factory := &mockLoopFactory{}
	mgr := newTestManager(factory)

	spawnTool := newSessionsSpawnTool(mgr, "parent-1", "")

	out, err := spawnTool.Execute(context.Background(), json.RawMessage(`{invalid`), tool.ExecutionEnv{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !out.IsError {
		t.Error("expected error output for invalid args")
	}
	if !strings.Contains(out.Content, "invalid arguments") {
		t.Errorf("expected 'invalid arguments' in content, got %q", out.Content)
	}
}

func TestSessionsListTool_Execute(t *testing.T) {
	t.Parallel()

	factory := &mockLoopFactory{
		resp: provider.CompletionResponse{
			Content:      "done",
			FinishReason: provider.FinishReasonStop,
		},
	}
	mgr := newTestManager(factory)

	// Spawn an agent so the list has something.
	_, spawnErr := mgr.Spawn(context.Background(), SpawnRequest{
		ParentID:       "parent-1",
		SystemPrompt:   "test",
		InitialMessage: "msg",
	})
	if spawnErr != nil {
		t.Fatalf("Spawn returned error: %v", spawnErr)
	}

	// Wait for completion.
	time.Sleep(200 * time.Millisecond)

	listTool := newSessionsListTool(mgr, "parent-1")

	out, err := listTool.Execute(context.Background(), json.RawMessage(`{}`), tool.ExecutionEnv{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if out.IsError {
		t.Fatalf("Execute returned error output: %s", out.Content)
	}

	var snapshots []Snap
	if unmarshalErr := json.Unmarshal([]byte(out.Content), &snapshots); unmarshalErr != nil {
		t.Fatalf("failed to unmarshal output: %v", unmarshalErr)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	if snapshots[0].ParentID != "parent-1" {
		t.Errorf("ParentID = %q, want %q", snapshots[0].ParentID, "parent-1")
	}
}

func TestSessionsHistoryTool_Execute(t *testing.T) {
	t.Parallel()

	factory := &mockLoopFactory{
		resp: provider.CompletionResponse{
			Content:      "result",
			FinishReason: provider.FinishReasonStop,
		},
	}
	mgr := newTestManager(factory)

	id, spawnErr := mgr.Spawn(context.Background(), SpawnRequest{
		ParentID:       "parent-1",
		SystemPrompt:   "test",
		InitialMessage: "hello",
	})
	if spawnErr != nil {
		t.Fatalf("Spawn returned error: %v", spawnErr)
	}

	// Wait for completion.
	time.Sleep(200 * time.Millisecond)

	histTool := newSessionsHistoryTool(mgr)

	args, _ := json.Marshal(historyArgs{ID: id})
	out, err := histTool.Execute(context.Background(), args, tool.ExecutionEnv{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if out.IsError {
		t.Fatalf("Execute returned error output: %s", out.Content)
	}

	var snap Snap
	if unmarshalErr := json.Unmarshal([]byte(out.Content), &snap); unmarshalErr != nil {
		t.Fatalf("failed to unmarshal output: %v", unmarshalErr)
	}
	if snap.ID != id {
		t.Errorf("ID = %q, want %q", snap.ID, id)
	}
	if snap.Status != StatusCompleted {
		t.Errorf("Status = %q, want %q", snap.Status, StatusCompleted)
	}
}

func TestSessionsHistoryTool_Execute_NotFound(t *testing.T) {
	t.Parallel()

	factory := &mockLoopFactory{}
	mgr := newTestManager(factory)

	histTool := newSessionsHistoryTool(mgr)

	args, _ := json.Marshal(historyArgs{ID: "nonexistent"})
	out, err := histTool.Execute(context.Background(), args, tool.ExecutionEnv{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !out.IsError {
		t.Error("expected error output for nonexistent ID")
	}
	if !strings.Contains(out.Content, "not found") {
		t.Errorf("expected 'not found' in content, got %q", out.Content)
	}
}

func TestSessionsKillTool_Execute(t *testing.T) {
	t.Parallel()

	// Use slow factory so agent stays running.
	factory := &slowLoopFactory{}
	mgr := NewManager(ManagerConfig{
		MaxConcurrent:  5,
		DefaultTimeout: 30 * time.Second,
		LoopFactory:    factory,
		Now:            fixedTime,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	id, spawnErr := mgr.Spawn(ctx, SpawnRequest{
		ParentID:       "parent-1",
		SystemPrompt:   "test",
		InitialMessage: "msg",
	})
	if spawnErr != nil {
		t.Fatalf("Spawn returned error: %v", spawnErr)
	}

	// Give goroutine time to start.
	time.Sleep(50 * time.Millisecond)

	killTool := newSessionsKillTool(mgr)

	args, _ := json.Marshal(killArgs{ID: id})
	out, err := killTool.Execute(context.Background(), args, tool.ExecutionEnv{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if out.IsError {
		t.Fatalf("Execute returned error output: %s", out.Content)
	}

	var result map[string]bool
	if unmarshalErr := json.Unmarshal([]byte(out.Content), &result); unmarshalErr != nil {
		t.Fatalf("failed to unmarshal output: %v", unmarshalErr)
	}
	if !result["ok"] {
		t.Error("expected ok=true in result")
	}

	// Verify the agent is killed.
	snap, _ := mgr.History(id)
	if snap.Status != StatusKilled {
		t.Errorf("Status = %q, want %q", snap.Status, StatusKilled)
	}
}

func TestSessionsSendTool_Execute(t *testing.T) {
	t.Parallel()

	// Use slow factory so agent stays running.
	factory := &slowLoopFactory{}
	mgr := NewManager(ManagerConfig{
		MaxConcurrent:  5,
		DefaultTimeout: 30 * time.Second,
		LoopFactory:    factory,
		Now:            fixedTime,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	id, spawnErr := mgr.Spawn(ctx, SpawnRequest{
		ParentID:       "parent-1",
		SystemPrompt:   "test",
		InitialMessage: "first",
	})
	if spawnErr != nil {
		t.Fatalf("Spawn returned error: %v", spawnErr)
	}

	// Give goroutine time to start.
	time.Sleep(50 * time.Millisecond)

	sendTool := newSessionsSendTool(mgr)

	args, _ := json.Marshal(sendArgs{ID: id, Message: "second"})
	out, err := sendTool.Execute(context.Background(), args, tool.ExecutionEnv{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if out.IsError {
		t.Fatalf("Execute returned error output: %s", out.Content)
	}

	var result map[string]bool
	if unmarshalErr := json.Unmarshal([]byte(out.Content), &result); unmarshalErr != nil {
		t.Fatalf("failed to unmarshal output: %v", unmarshalErr)
	}
	if !result["ok"] {
		t.Error("expected ok=true in result")
	}

	// Verify message was appended to history.
	snap, _ := mgr.History(id)
	if len(snap.History) < 2 {
		t.Fatalf("History len = %d, want >= 2", len(snap.History))
	}
	if snap.History[1].Content != "second" {
		t.Errorf("History[1].Content = %q, want %q", snap.History[1].Content, "second")
	}
}

func TestSessionsSendTool_Execute_NotFound(t *testing.T) {
	t.Parallel()

	factory := &mockLoopFactory{}
	mgr := newTestManager(factory)

	sendTool := newSessionsSendTool(mgr)

	args, _ := json.Marshal(sendArgs{ID: "nonexistent", Message: "msg"})
	out, err := sendTool.Execute(context.Background(), args, tool.ExecutionEnv{})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !out.IsError {
		t.Error("expected error output for nonexistent ID")
	}
	if !strings.Contains(out.Content, "not found") {
		t.Errorf("expected 'not found' in content, got %q", out.Content)
	}
}
