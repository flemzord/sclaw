// Package tooltest provides test helpers and mocks for the tool package.
package tooltest

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/flemzord/sclaw/internal/tool"
)

// MockTool is a configurable mock implementation of tool.Tool.
type MockTool struct {
	NameFunc          func() string
	DescriptionFunc   func() string
	SchemaFunc        func() json.RawMessage
	ScopesFunc        func() []tool.Scope
	DefaultPolicyFunc func() tool.ApprovalLevel
	ExecuteFunc       func(ctx context.Context, args json.RawMessage, env tool.ExecutionEnv) (tool.Output, error)

	mu           sync.Mutex
	ExecuteCalls int
}

// Name implements tool.Tool.
func (m *MockTool) Name() string {
	if m.NameFunc != nil {
		return m.NameFunc()
	}
	return "mock-tool"
}

// Description implements tool.Tool.
func (m *MockTool) Description() string {
	if m.DescriptionFunc != nil {
		return m.DescriptionFunc()
	}
	return "a mock tool"
}

// Schema implements tool.Tool.
func (m *MockTool) Schema() json.RawMessage {
	if m.SchemaFunc != nil {
		return m.SchemaFunc()
	}
	return json.RawMessage(`{}`)
}

// Scopes implements tool.Tool.
func (m *MockTool) Scopes() []tool.Scope {
	if m.ScopesFunc != nil {
		return m.ScopesFunc()
	}
	return []tool.Scope{tool.ScopeReadOnly}
}

// DefaultPolicy implements tool.Tool.
func (m *MockTool) DefaultPolicy() tool.ApprovalLevel {
	if m.DefaultPolicyFunc != nil {
		return m.DefaultPolicyFunc()
	}
	return tool.ApprovalAsk
}

// Execute implements tool.Tool.
func (m *MockTool) Execute(ctx context.Context, args json.RawMessage, env tool.ExecutionEnv) (tool.Output, error) {
	m.mu.Lock()
	m.ExecuteCalls++
	m.mu.Unlock()

	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, args, env)
	}
	return tool.Output{Content: "ok"}, nil
}

// MockApprovalRequester is a configurable mock for tool.ApprovalRequester.
type MockApprovalRequester struct {
	RequestApprovalFunc func(ctx context.Context, req tool.ApprovalRequest) (tool.ApprovalResponse, error)

	mu                   sync.Mutex
	RequestApprovalCalls int
}

// RequestApproval implements tool.ApprovalRequester.
func (m *MockApprovalRequester) RequestApproval(ctx context.Context, req tool.ApprovalRequest) (tool.ApprovalResponse, error) {
	m.mu.Lock()
	m.RequestApprovalCalls++
	m.mu.Unlock()

	if m.RequestApprovalFunc != nil {
		return m.RequestApprovalFunc(ctx, req)
	}
	return tool.ApprovalResponse{Approved: true}, nil
}

// SimpleTool creates a minimal tool for testing with the given name and default policy.
func SimpleTool(name string, policy tool.ApprovalLevel) *MockTool {
	return &MockTool{
		NameFunc:          func() string { return name },
		DescriptionFunc:   func() string { return "simple test tool: " + name },
		DefaultPolicyFunc: func() tool.ApprovalLevel { return policy },
		SchemaFunc:        func() json.RawMessage { return json.RawMessage(`{"type":"object"}`) },
		ScopesFunc:        func() []tool.Scope { return []tool.Scope{tool.ScopeReadOnly} },
		ExecuteFunc: func(_ context.Context, _ json.RawMessage, _ tool.ExecutionEnv) (tool.Output, error) {
			return tool.Output{Content: "executed: " + name}, nil
		},
	}
}

// Interface guards.
var (
	_ tool.Tool              = (*MockTool)(nil)
	_ tool.ApprovalRequester = (*MockApprovalRequester)(nil)
)
