package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type registryTestTool struct {
	name         string
	scopes       []Scope
	output       Output
	executeErr   error
	executeCalls *int
}

func (t registryTestTool) Name() string                 { return t.name }
func (t registryTestTool) Description() string          { return "registry test tool" }
func (t registryTestTool) Schema() json.RawMessage      { return json.RawMessage(`{}`) }
func (t registryTestTool) Scopes() []Scope              { return t.scopes }
func (t registryTestTool) DefaultPolicy() ApprovalLevel { return ApprovalAllow }
func (t registryTestTool) Execute(context.Context, json.RawMessage, ExecutionEnv) (Output, error) {
	if t.executeCalls != nil {
		*t.executeCalls = *t.executeCalls + 1
	}
	if t.executeErr != nil {
		return Output{}, t.executeErr
	}
	if t.output.Content != "" || t.output.IsError {
		return t.output, nil
	}
	return Output{Content: "ok"}, nil
}

func TestRegistryRegister_EmptyName(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	err := r.Register(registryTestTool{name: "", scopes: []Scope{ScopeReadOnly}})
	if !errors.Is(err, ErrEmptyToolName) {
		t.Fatalf("expected ErrEmptyToolName, got %v", err)
	}
}

func TestRegistryRegister_WhitespaceName(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	err := r.Register(registryTestTool{name: "   ", scopes: []Scope{ScopeReadOnly}})
	if !errors.Is(err, ErrEmptyToolName) {
		t.Fatalf("expected ErrEmptyToolName, got %v", err)
	}
}

func TestRegistryRegister_NoScopes(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	err := r.Register(registryTestTool{name: "read_file", scopes: nil})
	if !errors.Is(err, ErrNoScopes) {
		t.Fatalf("expected ErrNoScopes, got %v", err)
	}
}

func TestRegistryRegister_Duplicate(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	t1 := registryTestTool{name: "read_file", scopes: []Scope{ScopeReadOnly}}
	if err := r.Register(t1); err != nil {
		t.Fatalf("unexpected first register error: %v", err)
	}

	err := r.Register(t1)
	if !errors.Is(err, ErrDuplicateTool) {
		t.Fatalf("expected ErrDuplicateTool, got %v", err)
	}
}

func TestRegistrySchemas_UsesCanonicalRegisteredName(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	if err := r.Register(registryTestTool{name: " read_file ", scopes: []Scope{ScopeReadOnly}}); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}

	schemas := r.Schemas()
	if len(schemas) != 1 {
		t.Fatalf("got %d schemas, want 1", len(schemas))
	}
	if schemas[0].Name != "read_file" {
		t.Fatalf("schema name = %q, want %q", schemas[0].Name, "read_file")
	}
}

func TestRegistryExecute_AllowExecutes(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	calls := 0
	if err := r.Register(registryTestTool{
		name:         "read_file",
		scopes:       []Scope{ScopeReadOnly},
		executeCalls: &calls,
		output:       Output{Content: "done"},
	}); err != nil {
		t.Fatalf("register error: %v", err)
	}

	out, err := r.Execute(
		context.Background(),
		"read_file",
		nil,
		PolicyConfig{DM: Policy{Tools: map[string]ApprovalLevel{"read_file": ApprovalAllow}}},
		PolicyContextDM,
		nil,
		nil,
		time.Second,
		ExecutionEnv{},
	)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if out.Content != "done" {
		t.Fatalf("output = %q, want %q", out.Content, "done")
	}
	if calls != 1 {
		t.Fatalf("execute calls = %d, want 1", calls)
	}
}

func TestRegistryExecute_DenySkipsExecution(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	calls := 0
	if err := r.Register(registryTestTool{
		name:         "exec_cmd",
		scopes:       []Scope{ScopeExec},
		executeCalls: &calls,
	}); err != nil {
		t.Fatalf("register error: %v", err)
	}

	_, err := r.Execute(
		context.Background(),
		"exec_cmd",
		nil,
		PolicyConfig{DM: Policy{Tools: map[string]ApprovalLevel{"exec_cmd": ApprovalDeny}}},
		PolicyContextDM,
		nil,
		nil,
		time.Second,
		ExecutionEnv{},
	)
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("expected ErrDenied, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("execute calls = %d, want 0", calls)
	}
}

func TestRegistryExecute_AskApprovedExecutes(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	calls := 0
	if err := r.Register(registryTestTool{
		name:         "write_file",
		scopes:       []Scope{ScopeReadWrite},
		executeCalls: &calls,
	}); err != nil {
		t.Fatalf("register error: %v", err)
	}

	requester := &fakeRequester{
		respondFunc: func(_ context.Context, _ ApprovalRequest) (ApprovalResponse, error) {
			return ApprovalResponse{Approved: true, Reason: "ok"}, nil
		},
	}

	_, err := r.Execute(
		context.Background(),
		"write_file",
		json.RawMessage(`{"path":"a.txt"}`),
		PolicyConfig{DM: Policy{Tools: map[string]ApprovalLevel{"write_file": ApprovalAsk}}},
		PolicyContextDM,
		nil,
		requester,
		time.Second,
		ExecutionEnv{},
	)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("execute calls = %d, want 1", calls)
	}
}

func TestRegistryExecute_AskDeniedSkipsExecution(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	calls := 0
	if err := r.Register(registryTestTool{
		name:         "delete_file",
		scopes:       []Scope{ScopeReadWrite},
		executeCalls: &calls,
	}); err != nil {
		t.Fatalf("register error: %v", err)
	}

	requester := &fakeRequester{
		respondFunc: func(_ context.Context, _ ApprovalRequest) (ApprovalResponse, error) {
			return ApprovalResponse{Approved: false, Reason: "nope"}, nil
		},
	}

	_, err := r.Execute(
		context.Background(),
		"delete_file",
		nil,
		PolicyConfig{DM: Policy{Tools: map[string]ApprovalLevel{"delete_file": ApprovalAsk}}},
		PolicyContextDM,
		nil,
		requester,
		time.Second,
		ExecutionEnv{},
	)
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("expected ErrDenied, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("execute calls = %d, want 0", calls)
	}
}

func TestRegistryExecute_AskTimeout(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	calls := 0
	if err := r.Register(registryTestTool{
		name:         "network_call",
		scopes:       []Scope{ScopeNetwork},
		executeCalls: &calls,
	}); err != nil {
		t.Fatalf("register error: %v", err)
	}

	requester := &fakeRequester{
		respondFunc: func(ctx context.Context, _ ApprovalRequest) (ApprovalResponse, error) {
			<-ctx.Done()
			return ApprovalResponse{}, ctx.Err()
		},
	}

	_, err := r.Execute(
		context.Background(),
		"network_call",
		nil,
		PolicyConfig{DM: Policy{Tools: map[string]ApprovalLevel{"network_call": ApprovalAsk}}},
		PolicyContextDM,
		nil,
		requester,
		25*time.Millisecond,
		ExecutionEnv{},
	)
	if !errors.Is(err, ErrApprovalTimeout) {
		t.Fatalf("expected ErrApprovalTimeout, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("execute calls = %d, want 0", calls)
	}
}

func TestRegistryExecute_ElevatedAskBecomesAllow(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	calls := 0
	if err := r.Register(registryTestTool{
		name:         "write_file",
		scopes:       []Scope{ScopeReadWrite},
		executeCalls: &calls,
	}); err != nil {
		t.Fatalf("register error: %v", err)
	}

	elevated := NewElevatedState()
	elevated.Elevate(time.Minute)

	_, err := r.Execute(
		context.Background(),
		"write_file",
		nil,
		PolicyConfig{DM: Policy{Tools: map[string]ApprovalLevel{"write_file": ApprovalAsk}}},
		PolicyContextDM,
		elevated,
		nil,
		time.Second,
		ExecutionEnv{},
	)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("execute calls = %d, want 1", calls)
	}
}

func TestRegistryExecute_ElevatedDenyUnchanged(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	calls := 0
	if err := r.Register(registryTestTool{
		name:         "rm_rf",
		scopes:       []Scope{ScopeExec},
		executeCalls: &calls,
	}); err != nil {
		t.Fatalf("register error: %v", err)
	}

	elevated := NewElevatedState()
	elevated.Elevate(time.Minute)

	_, err := r.Execute(
		context.Background(),
		"rm_rf",
		nil,
		PolicyConfig{DM: Policy{Tools: map[string]ApprovalLevel{"rm_rf": ApprovalDeny}}},
		PolicyContextDM,
		elevated,
		nil,
		time.Second,
		ExecutionEnv{},
	)
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("expected ErrDenied, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("execute calls = %d, want 0", calls)
	}
}

func TestRegistryExecute_ContextAwarePolicy(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	dmCalls := 0
	if err := r.Register(registryTestTool{
		name:         "send_msg",
		scopes:       []Scope{ScopeNetwork},
		executeCalls: &dmCalls,
	}); err != nil {
		t.Fatalf("register error: %v", err)
	}

	cfg := PolicyConfig{
		DM:    Policy{Default: ApprovalAllow},
		Group: Policy{Default: ApprovalDeny},
	}

	if _, err := r.Execute(context.Background(), "send_msg", nil, cfg, PolicyContextDM, nil, nil, time.Second, ExecutionEnv{}); err != nil {
		t.Fatalf("dm execute error: %v", err)
	}
	if dmCalls != 1 {
		t.Fatalf("dm execute calls = %d, want 1", dmCalls)
	}

	if _, err := r.Execute(context.Background(), "send_msg", nil, cfg, PolicyContextGroup, nil, nil, time.Second, ExecutionEnv{}); !errors.Is(err, ErrDenied) {
		t.Fatalf("group expected ErrDenied, got %v", err)
	}
	if dmCalls != 1 {
		t.Fatalf("group should not execute, calls = %d", dmCalls)
	}
}
