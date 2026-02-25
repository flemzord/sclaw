// Package hooktest provides test doubles for the hook package.
package hooktest

import (
	"context"
	"sync"
	"time"

	"github.com/flemzord/sclaw/internal/hook"
)

// MockHook is a configurable test double for hook.Hook.
type MockHook struct {
	PositionVal hook.Position
	PriorityVal int
	ExecuteFunc func(ctx context.Context, hctx *hook.Context) (hook.Action, error)

	mu    sync.Mutex
	Calls int
}

// Compile-time interface check.
var _ hook.Hook = (*MockHook)(nil)

// Position returns the configured position.
func (m *MockHook) Position() hook.Position { return m.PositionVal }

// Priority returns the configured priority.
func (m *MockHook) Priority() int { return m.PriorityVal }

// Execute delegates to ExecuteFunc and increments the call counter.
func (m *MockHook) Execute(ctx context.Context, hctx *hook.Context) (hook.Action, error) {
	m.mu.Lock()
	m.Calls++
	m.mu.Unlock()

	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, hctx)
	}
	return hook.ActionContinue, nil
}

// CallCount returns the number of times Execute was called.
func (m *MockHook) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Calls
}

// MockSessionView is a simple field-based implementation of hook.SessionView.
type MockSessionView struct {
	SessionIDVal string
	Channel      string
	ChatID       string
	ThreadID     string
	AgentIDVal   string
	CreatedAtVal time.Time
	MetadataMap  map[string]any
}

// Compile-time interface check.
var _ hook.SessionView = (*MockSessionView)(nil)

// SessionID implements hook.SessionView.
func (m *MockSessionView) SessionID() string { return m.SessionIDVal }

// SessionKey implements hook.SessionView.
func (m *MockSessionView) SessionKey() (string, string, string) {
	return m.Channel, m.ChatID, m.ThreadID
}

// AgentID implements hook.SessionView.
func (m *MockSessionView) AgentID() string { return m.AgentIDVal }

// CreatedAt implements hook.SessionView.
func (m *MockSessionView) CreatedAt() time.Time { return m.CreatedAtVal }

// GetMetadata implements hook.SessionView.
func (m *MockSessionView) GetMetadata(key string) (any, bool) {
	if m.MetadataMap == nil {
		return nil, false
	}
	v, ok := m.MetadataMap[key]
	return v, ok
}
