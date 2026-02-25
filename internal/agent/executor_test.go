package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/tool"
)

// mockTool implements tool.Tool for testing across the agent package.
// It supports configurable output, errors, panics, and execution delay.
type mockTool struct {
	name      string
	output    tool.Output
	err       error
	panicMsg  string
	execDelay time.Duration
}

func (m *mockTool) Name() string                      { return m.name }
func (m *mockTool) Description() string               { return "mock tool" }
func (m *mockTool) Schema() json.RawMessage           { return json.RawMessage(`{}`) }
func (m *mockTool) Scopes() []tool.Scope              { return []tool.Scope{tool.ScopeReadOnly} }
func (m *mockTool) DefaultPolicy() tool.ApprovalLevel { return tool.ApprovalAllow }

func (m *mockTool) Execute(_ context.Context, _ json.RawMessage, _ tool.ExecutionEnv) (tool.Output, error) {
	if m.execDelay > 0 {
		time.Sleep(m.execDelay)
	}
	if m.panicMsg != "" {
		panic(m.panicMsg)
	}
	return m.output, m.err
}

func newTestExecutor(reg *tool.Registry) *ToolExecutor {
	return NewToolExecutor(ToolExecutorConfig{
		Registry: reg,
		PolicyCfg: tool.PolicyConfig{
			DM: tool.Policy{Default: tool.ApprovalAllow},
		},
		PolicyCtx: tool.PolicyContextDM,
	})
}

func newTestExecutorFromTools(tools ...*mockTool) *ToolExecutor {
	reg := tool.NewRegistry()
	for _, t := range tools {
		if err := reg.Register(t); err != nil {
			panic(err)
		}
	}
	return newTestExecutor(reg)
}

func tc(id, name string) provider.ToolCall {
	return provider.ToolCall{
		ID:        id,
		Name:      name,
		Arguments: json.RawMessage(`{}`),
	}
}

func TestExecute_SingleSuccess(t *testing.T) {
	t.Parallel()

	mt := &mockTool{
		name:   "echo",
		output: tool.Output{Content: "hello", IsError: false},
	}
	exec := newTestExecutorFromTools(mt)
	results := exec.Execute(context.Background(), []provider.ToolCall{tc("c1", "echo")})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.ID != "c1" {
		t.Errorf("ID = %q, want c1", r.ID)
	}
	if r.Output.Content != "hello" {
		t.Errorf("Content = %q, want hello", r.Output.Content)
	}
	if r.Output.IsError {
		t.Error("expected no error")
	}
	if r.Panicked {
		t.Error("expected no panic")
	}
}

func TestExecute_ParallelExecution(t *testing.T) {
	t.Parallel()

	delay := 100 * time.Millisecond
	tools := []*mockTool{
		{name: "tool1", output: tool.Output{Content: "tool1"}, execDelay: delay},
		{name: "tool2", output: tool.Output{Content: "tool2"}, execDelay: delay},
		{name: "tool3", output: tool.Output{Content: "tool3"}, execDelay: delay},
	}
	exec := newTestExecutorFromTools(tools...)

	start := time.Now()
	results := exec.Execute(context.Background(), []provider.ToolCall{
		tc("c1", "tool1"),
		tc("c2", "tool2"),
		tc("c3", "tool3"),
	})
	elapsed := time.Since(start)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// Sequential would take 3*delay; parallel should take roughly 1*delay.
	maxExpected := 2 * delay
	if elapsed > maxExpected {
		t.Errorf("elapsed %v suggests sequential execution (want < %v)", elapsed, maxExpected)
	}
}

func TestExecute_PanicRecovery(t *testing.T) {
	t.Parallel()

	mt := &mockTool{
		name:     "panicker",
		panicMsg: "something went wrong",
	}
	exec := newTestExecutorFromTools(mt)
	results := exec.Execute(context.Background(), []provider.ToolCall{tc("c1", "panicker")})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if !r.Panicked {
		t.Error("expected Panicked=true")
	}
	if !r.Output.IsError {
		t.Error("expected IsError=true after panic")
	}
	if r.Output.Content == "" {
		t.Error("expected non-empty Content after panic")
	}
}

func TestExecute_ToolError(t *testing.T) {
	t.Parallel()

	mt := &mockTool{
		name: "failer",
		err:  errors.New("execution failed"),
	}
	exec := newTestExecutorFromTools(mt)
	results := exec.Execute(context.Background(), []provider.ToolCall{tc("c1", "failer")})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if !r.Output.IsError {
		t.Error("expected IsError=true")
	}
	if r.Output.Content != "execution failed" {
		t.Errorf("Content = %q, want %q", r.Output.Content, "execution failed")
	}
	if r.Panicked {
		t.Error("expected Panicked=false for normal error")
	}
}

func TestExecute_ToolNotFound(t *testing.T) {
	t.Parallel()

	reg := tool.NewRegistry()
	exec := newTestExecutor(reg)
	results := exec.Execute(context.Background(), []provider.ToolCall{tc("c1", "nonexistent")})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if !r.Output.IsError {
		t.Error("expected IsError=true for missing tool")
	}
	if r.Output.Content == "" {
		t.Error("expected non-empty error content")
	}
}

func TestExecute_MaintainsOrder(t *testing.T) {
	t.Parallel()

	names := []string{"alpha", "beta", "gamma", "delta"}
	tools := make([]*mockTool, 0, len(names))
	for i, name := range names {
		// Tools with longer delays registered first finish last — verifies order is preserved.
		delay := time.Duration(len(names)-i) * 10 * time.Millisecond
		tools = append(tools, &mockTool{
			name:      name,
			output:    tool.Output{Content: name},
			execDelay: delay,
		})
	}
	exec := newTestExecutorFromTools(tools...)

	calls := make([]provider.ToolCall, len(names))
	for i, name := range names {
		calls[i] = tc(name+"-id", name)
	}

	results := exec.Execute(context.Background(), calls)

	if len(results) != len(names) {
		t.Fatalf("expected %d results, got %d", len(names), len(results))
	}
	for i, name := range names {
		if results[i].Name != name {
			t.Errorf("results[%d].Name = %q, want %q", i, results[i].Name, name)
		}
		if results[i].Output.Content != name {
			t.Errorf("results[%d].Content = %q, want %q", i, results[i].Output.Content, name)
		}
	}
}

// TestExecute_ParallelErrorIsolation verifies that one tool erroring/panicking
// does not block or affect the results of other parallel tools.
func TestExecute_ParallelErrorIsolation(t *testing.T) {
	t.Parallel()

	tools := []*mockTool{
		{name: "good1", output: tool.Output{Content: "ok1"}},
		{name: "panicker", panicMsg: "boom"},
		{name: "good2", output: tool.Output{Content: "ok2"}},
	}
	exec := newTestExecutorFromTools(tools...)

	results := exec.Execute(context.Background(), []provider.ToolCall{
		tc("c1", "good1"),
		tc("c2", "panicker"),
		tc("c3", "good2"),
	})

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// good1 succeeded.
	if results[0].Output.IsError || results[0].Output.Content != "ok1" {
		t.Errorf("results[0]: expected success with 'ok1', got error=%v content=%q",
			results[0].Output.IsError, results[0].Output.Content)
	}

	// panicker failed.
	if !results[1].Panicked || !results[1].Output.IsError {
		t.Errorf("results[1]: expected panic, got panicked=%v error=%v",
			results[1].Panicked, results[1].Output.IsError)
	}

	// good2 succeeded despite panicker.
	if results[2].Output.IsError || results[2].Output.Content != "ok2" {
		t.Errorf("results[2]: expected success with 'ok2', got error=%v content=%q",
			results[2].Output.IsError, results[2].Output.Content)
	}
}
