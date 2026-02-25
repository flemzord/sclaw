package subagent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/tool"
)

// mockProvider returns pre-configured responses for testing.
type mockProvider struct {
	response provider.CompletionResponse
	err      error
}

func (p *mockProvider) Complete(_ context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
	return p.response, p.err
}

func (p *mockProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	return nil, errors.New("not implemented")
}

func (p *mockProvider) ContextWindowSize() int { return 128000 }
func (p *mockProvider) ModelName() string      { return "mock-model" }

// mockLoopFactory creates agent loops backed by mockProvider.
type mockLoopFactory struct {
	resp provider.CompletionResponse
	err  error
}

func (f *mockLoopFactory) NewLoop(_ string) (*agent.Loop, error) {
	p := &mockProvider{response: f.resp, err: f.err}
	reg := tool.NewRegistry()
	executor := agent.NewToolExecutor(agent.ToolExecutorConfig{
		Registry: reg,
		PolicyCfg: tool.PolicyConfig{
			DM: tool.Policy{Default: tool.ApprovalAllow},
		},
		PolicyCtx: tool.PolicyContextDM,
	})
	return agent.NewLoop(p, executor, agent.LoopConfig{}), nil
}

// failingLoopFactory returns an error from NewLoop.
type failingLoopFactory struct{}

func (f *failingLoopFactory) NewLoop(_ string) (*agent.Loop, error) {
	return nil, errors.New("factory error")
}

// slowLoopFactory creates loops that block until context is done.
type slowLoopFactory struct{}

func (f *slowLoopFactory) NewLoop(_ string) (*agent.Loop, error) {
	p := &blockingProvider{}
	reg := tool.NewRegistry()
	executor := agent.NewToolExecutor(agent.ToolExecutorConfig{
		Registry: reg,
		PolicyCfg: tool.PolicyConfig{
			DM: tool.Policy{Default: tool.ApprovalAllow},
		},
		PolicyCtx: tool.PolicyContextDM,
	})
	return agent.NewLoop(p, executor, agent.LoopConfig{
		Timeout: 10 * time.Minute, // Use a long timeout so context cancellation controls termination.
	}), nil
}

// blockingProvider blocks until the context is cancelled.
type blockingProvider struct{}

func (p *blockingProvider) Complete(ctx context.Context, _ provider.CompletionRequest) (provider.CompletionResponse, error) {
	<-ctx.Done()
	return provider.CompletionResponse{}, ctx.Err()
}

func (p *blockingProvider) Stream(_ context.Context, _ provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	return nil, errors.New("not implemented")
}

func (p *blockingProvider) ContextWindowSize() int { return 128000 }
func (p *blockingProvider) ModelName() string      { return "blocking-model" }

func fixedTime() time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}

func newTestManager(factory LoopFactory) *Manager {
	return NewManager(ManagerConfig{
		MaxConcurrent:  5,
		DefaultTimeout: 5 * time.Second,
		MaxHistory:     50,
		LoopFactory:    factory,
		Now:            fixedTime,
	})
}

func TestManager_Spawn_Success(t *testing.T) {
	t.Parallel()

	factory := &mockLoopFactory{
		resp: provider.CompletionResponse{
			Content:      "sub-agent done",
			FinishReason: provider.FinishReasonStop,
		},
	}
	mgr := newTestManager(factory)

	id, err := mgr.Spawn(context.Background(), SpawnRequest{
		ParentID:       "parent-1",
		SystemPrompt:   "You are a helper.",
		InitialMessage: "Do something.",
	})
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}
	if id == "" {
		t.Fatal("Spawn returned empty ID")
	}
	if len(id) != 32 {
		t.Errorf("ID length = %d, want 32 hex chars", len(id))
	}

	// Wait for the agent goroutine to complete.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap, snapErr := mgr.History(id)
		if snapErr != nil {
			t.Fatalf("History returned error: %v", snapErr)
		}
		if snap.Status != StatusRunning {
			// Agent has finished.
			if snap.Status != StatusCompleted {
				t.Errorf("Status = %q, want %q", snap.Status, StatusCompleted)
			}
			if snap.Result == nil {
				t.Fatal("Result is nil after completion")
			}
			if snap.Result.Content != "sub-agent done" {
				t.Errorf("Result.Content = %q, want %q", snap.Result.Content, "sub-agent done")
			}
			if snap.ParentID != "parent-1" {
				t.Errorf("ParentID = %q, want %q", snap.ParentID, "parent-1")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("sub-agent did not complete within timeout")
}

func TestManager_Spawn_RecursiveBlocked(t *testing.T) {
	t.Parallel()

	factory := &mockLoopFactory{}
	mgr := newTestManager(factory)

	_, err := mgr.Spawn(context.Background(), SpawnRequest{
		ParentID:       "parent-1",
		SystemPrompt:   "test",
		InitialMessage: "test",
		IsSubAgent:     true,
	})
	if !errors.Is(err, ErrRecursiveSpawn) {
		t.Fatalf("expected ErrRecursiveSpawn, got %v", err)
	}
}

func TestManager_Spawn_MaxConcurrent(t *testing.T) {
	t.Parallel()

	// Use a slow factory so agents stay running.
	factory := &slowLoopFactory{}
	mgr := NewManager(ManagerConfig{
		MaxConcurrent:  2,
		DefaultTimeout: 30 * time.Second,
		LoopFactory:    factory,
		Now:            fixedTime,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Spawn MaxConcurrent agents.
	for i := 0; i < 2; i++ {
		_, err := mgr.Spawn(ctx, SpawnRequest{
			ParentID:       "parent",
			SystemPrompt:   "test",
			InitialMessage: "msg",
		})
		if err != nil {
			t.Fatalf("Spawn %d returned error: %v", i, err)
		}
	}

	// Third spawn should fail.
	_, err := mgr.Spawn(ctx, SpawnRequest{
		ParentID:       "parent",
		SystemPrompt:   "test",
		InitialMessage: "msg",
	})
	if !errors.Is(err, ErrMaxConcurrent) {
		t.Fatalf("expected ErrMaxConcurrent, got %v", err)
	}
}

func TestManager_Spawn_FactoryError(t *testing.T) {
	t.Parallel()

	factory := &failingLoopFactory{}
	mgr := newTestManager(factory)

	id, err := mgr.Spawn(context.Background(), SpawnRequest{
		ParentID:       "parent-1",
		SystemPrompt:   "test",
		InitialMessage: "msg",
	})
	// ID is still returned even on factory failure.
	if err != nil {
		t.Fatalf("Spawn returned error: %v (should return nil with ID)", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID even on factory failure")
	}

	snap, snapErr := mgr.History(id)
	if snapErr != nil {
		t.Fatalf("History returned error: %v", snapErr)
	}
	if snap.Status != StatusFailed {
		t.Errorf("Status = %q, want %q", snap.Status, StatusFailed)
	}
	if snap.ErrorMsg == "" {
		t.Error("expected non-empty ErrorMsg on factory failure")
	}
}

func TestManager_List_FiltersByParent(t *testing.T) {
	t.Parallel()

	factory := &mockLoopFactory{
		resp: provider.CompletionResponse{
			Content:      "done",
			FinishReason: provider.FinishReasonStop,
		},
	}
	mgr := newTestManager(factory)

	// Spawn agents for parent A.
	for i := 0; i < 2; i++ {
		_, err := mgr.Spawn(context.Background(), SpawnRequest{
			ParentID:       "parent-A",
			SystemPrompt:   "test",
			InitialMessage: "msg",
		})
		if err != nil {
			t.Fatalf("Spawn for parent-A returned error: %v", err)
		}
	}

	// Spawn agent for parent B.
	_, err := mgr.Spawn(context.Background(), SpawnRequest{
		ParentID:       "parent-B",
		SystemPrompt:   "test",
		InitialMessage: "msg",
	})
	if err != nil {
		t.Fatalf("Spawn for parent-B returned error: %v", err)
	}

	// Wait for all agents to complete.
	time.Sleep(200 * time.Millisecond)

	listA := mgr.List("parent-A")
	if len(listA) != 2 {
		t.Errorf("List(parent-A) returned %d agents, want 2", len(listA))
	}
	for _, snap := range listA {
		if snap.ParentID != "parent-A" {
			t.Errorf("snap.ParentID = %q, want parent-A", snap.ParentID)
		}
	}

	listB := mgr.List("parent-B")
	if len(listB) != 1 {
		t.Errorf("List(parent-B) returned %d agents, want 1", len(listB))
	}

	listC := mgr.List("parent-C")
	if len(listC) != 0 {
		t.Errorf("List(parent-C) returned %d agents, want 0", len(listC))
	}
}

func TestManager_History_NotFound(t *testing.T) {
	t.Parallel()

	factory := &mockLoopFactory{}
	mgr := newTestManager(factory)

	_, err := mgr.History("nonexistent-id")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestManager_Kill_Running(t *testing.T) {
	t.Parallel()

	// Use slow factory so the agent stays running.
	factory := &slowLoopFactory{}
	mgr := NewManager(ManagerConfig{
		MaxConcurrent:  5,
		DefaultTimeout: 30 * time.Second,
		LoopFactory:    factory,
		Now:            fixedTime,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	id, err := mgr.Spawn(ctx, SpawnRequest{
		ParentID:       "parent-1",
		SystemPrompt:   "test",
		InitialMessage: "msg",
	})
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Give the goroutine time to start.
	time.Sleep(50 * time.Millisecond)

	err = mgr.Kill(id)
	if err != nil {
		t.Fatalf("Kill returned error: %v", err)
	}

	snap, snapErr := mgr.History(id)
	if snapErr != nil {
		t.Fatalf("History returned error: %v", snapErr)
	}
	if snap.Status != StatusKilled {
		t.Errorf("Status = %q, want %q", snap.Status, StatusKilled)
	}
}

func TestManager_Kill_NotFound(t *testing.T) {
	t.Parallel()

	factory := &mockLoopFactory{}
	mgr := newTestManager(factory)

	err := mgr.Kill("nonexistent-id")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestManager_Kill_AlreadyFinished(t *testing.T) {
	t.Parallel()

	factory := &mockLoopFactory{
		resp: provider.CompletionResponse{
			Content:      "done",
			FinishReason: provider.FinishReasonStop,
		},
	}
	mgr := newTestManager(factory)

	id, err := mgr.Spawn(context.Background(), SpawnRequest{
		ParentID:       "parent-1",
		SystemPrompt:   "test",
		InitialMessage: "msg",
	})
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Wait for agent to complete.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap, _ := mgr.History(id)
		if snap.Status != StatusRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	err = mgr.Kill(id)
	if !errors.Is(err, ErrAlreadyFinished) {
		t.Fatalf("expected ErrAlreadyFinished, got %v", err)
	}
}

func TestManager_Shutdown(t *testing.T) {
	t.Parallel()

	// Use slow factory so agents stay running.
	factory := &slowLoopFactory{}
	mgr := NewManager(ManagerConfig{
		MaxConcurrent:  5,
		DefaultTimeout: 30 * time.Second,
		LoopFactory:    factory,
		Now:            fixedTime,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var ids []string
	for i := 0; i < 3; i++ {
		id, err := mgr.Spawn(ctx, SpawnRequest{
			ParentID:       "parent-1",
			SystemPrompt:   "test",
			InitialMessage: "msg",
		})
		if err != nil {
			t.Fatalf("Spawn %d returned error: %v", i, err)
		}
		ids = append(ids, id)
	}

	// Give goroutines time to start.
	time.Sleep(50 * time.Millisecond)

	mgr.Shutdown(context.Background())

	for _, id := range ids {
		snap, snapErr := mgr.History(id)
		if snapErr != nil {
			t.Fatalf("History(%s) returned error: %v", id, snapErr)
		}
		if snap.Status != StatusKilled {
			t.Errorf("agent %s: Status = %q, want %q", id, snap.Status, StatusKilled)
		}
	}
}

func TestManager_Send_Success(t *testing.T) {
	t.Parallel()

	// Use slow factory so the agent stays running.
	factory := &slowLoopFactory{}
	mgr := NewManager(ManagerConfig{
		MaxConcurrent:  5,
		DefaultTimeout: 30 * time.Second,
		LoopFactory:    factory,
		Now:            fixedTime,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	id, err := mgr.Spawn(ctx, SpawnRequest{
		ParentID:       "parent-1",
		SystemPrompt:   "test",
		InitialMessage: "first",
	})
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Give goroutine time to start.
	time.Sleep(50 * time.Millisecond)

	err = mgr.Send(context.Background(), id, "second message")
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	snap, snapErr := mgr.History(id)
	if snapErr != nil {
		t.Fatalf("History returned error: %v", snapErr)
	}

	// History should contain the initial message + the sent message.
	if len(snap.History) != 2 {
		t.Fatalf("History len = %d, want 2", len(snap.History))
	}
	if snap.History[1].Content != "second message" {
		t.Errorf("History[1].Content = %q, want %q", snap.History[1].Content, "second message")
	}
	if snap.History[1].Role != provider.MessageRoleUser {
		t.Errorf("History[1].Role = %q, want %q", snap.History[1].Role, provider.MessageRoleUser)
	}
}

func TestManager_Send_NotFound(t *testing.T) {
	t.Parallel()

	factory := &mockLoopFactory{}
	mgr := newTestManager(factory)

	err := mgr.Send(context.Background(), "nonexistent", "msg")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestManager_Send_NotRunning(t *testing.T) {
	t.Parallel()

	factory := &mockLoopFactory{
		resp: provider.CompletionResponse{
			Content:      "done",
			FinishReason: provider.FinishReasonStop,
		},
	}
	mgr := newTestManager(factory)

	id, err := mgr.Spawn(context.Background(), SpawnRequest{
		ParentID:       "parent-1",
		SystemPrompt:   "test",
		InitialMessage: "msg",
	})
	if err != nil {
		t.Fatalf("Spawn returned error: %v", err)
	}

	// Wait for agent to complete.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap, _ := mgr.History(id)
		if snap.Status != StatusRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	err = mgr.Send(context.Background(), id, "late message")
	if !errors.Is(err, ErrNotRunning) {
		t.Fatalf("expected ErrNotRunning, got %v", err)
	}
}
