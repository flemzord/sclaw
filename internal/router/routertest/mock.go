// Package routertest provides mock implementations of router interfaces for testing.
package routertest

import (
	"context"
	"sync"
	"time"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/memory"
	"github.com/flemzord/sclaw/internal/router"
	"github.com/flemzord/sclaw/pkg/message"
)

// MockResponseSender records sent messages for test assertions.
type MockResponseSender struct {
	SendFunc  func(ctx context.Context, msg message.OutboundMessage) error
	mu        sync.Mutex
	sent      []message.OutboundMessage
	sendCalls int
}

// Send records the outbound message and optionally delegates to SendFunc.
func (m *MockResponseSender) Send(ctx context.Context, msg message.OutboundMessage) error {
	m.mu.Lock()
	m.sendCalls++
	m.sent = append(m.sent, msg)
	m.mu.Unlock()
	if m.SendFunc != nil {
		return m.SendFunc(ctx, msg)
	}
	return nil
}

// SentMessages returns a copy of all recorded outbound messages.
// Safe for concurrent use.
func (m *MockResponseSender) SentMessages() []message.OutboundMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]message.OutboundMessage, len(m.sent))
	copy(cp, m.sent)
	return cp
}

// SendCallCount returns the number of times Send was called.
// Safe for concurrent use.
func (m *MockResponseSender) SendCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sendCalls
}

// MockAgentFactory returns agent loops for test sessions.
type MockAgentFactory struct {
	ForSessionFunc func(session *router.Session, msg message.InboundMessage) (*agent.Loop, error)
}

// ForSession delegates to ForSessionFunc if set, otherwise returns nil.
func (m *MockAgentFactory) ForSession(session *router.Session, msg message.InboundMessage) (*agent.Loop, error) {
	if m.ForSessionFunc != nil {
		return m.ForSessionFunc(session, msg)
	}
	return nil, nil
}

// MockSessionStore provides controllable session store behavior for tests.
type MockSessionStore struct {
	GetOrCreateFunc  func(key router.SessionKey) (*router.Session, bool)
	GetFunc          func(key router.SessionKey) *router.Session
	TouchFunc        func(key router.SessionKey)
	DeleteFunc       func(key router.SessionKey)
	PruneFunc        func(maxIdle time.Duration) int
	PruneByAgentFunc func(agentID string, maxIdle time.Duration) int
	LenFunc          func() int
	RangeFunc        func(fn func(router.SessionKey, *router.Session) bool)
}

// GetOrCreate delegates to GetOrCreateFunc if set, otherwise returns a default session.
func (m *MockSessionStore) GetOrCreate(key router.SessionKey) (*router.Session, bool) {
	if m.GetOrCreateFunc != nil {
		return m.GetOrCreateFunc(key)
	}
	return &router.Session{Key: key}, true
}

// Get delegates to GetFunc if set, otherwise returns nil.
func (m *MockSessionStore) Get(key router.SessionKey) *router.Session {
	if m.GetFunc != nil {
		return m.GetFunc(key)
	}
	return nil
}

// Touch delegates to TouchFunc if set, otherwise is a no-op.
func (m *MockSessionStore) Touch(key router.SessionKey) {
	if m.TouchFunc != nil {
		m.TouchFunc(key)
	}
}

// Delete delegates to DeleteFunc if set, otherwise is a no-op.
func (m *MockSessionStore) Delete(key router.SessionKey) {
	if m.DeleteFunc != nil {
		m.DeleteFunc(key)
	}
}

// Prune delegates to PruneFunc if set, otherwise returns 0.
func (m *MockSessionStore) Prune(maxIdle time.Duration) int {
	if m.PruneFunc != nil {
		return m.PruneFunc(maxIdle)
	}
	return 0
}

// PruneByAgent delegates to PruneByAgentFunc if set, otherwise returns 0.
func (m *MockSessionStore) PruneByAgent(agentID string, maxIdle time.Duration) int {
	if m.PruneByAgentFunc != nil {
		return m.PruneByAgentFunc(agentID, maxIdle)
	}
	return 0
}

// Len delegates to LenFunc if set, otherwise returns 0.
func (m *MockSessionStore) Len() int {
	if m.LenFunc != nil {
		return m.LenFunc()
	}
	return 0
}

// Range delegates to RangeFunc if set, otherwise is a no-op.
func (m *MockSessionStore) Range(fn func(router.SessionKey, *router.Session) bool) {
	if m.RangeFunc != nil {
		m.RangeFunc(fn)
	}
}

// MockHistoryResolver provides a controllable HistoryResolver for tests.
type MockHistoryResolver struct {
	ResolveHistoryFunc func(agentID string) memory.HistoryStore
}

// ResolveHistory delegates to ResolveHistoryFunc if set, otherwise returns nil.
func (m *MockHistoryResolver) ResolveHistory(agentID string) memory.HistoryStore {
	if m.ResolveHistoryFunc != nil {
		return m.ResolveHistoryFunc(agentID)
	}
	return nil
}

// MockSoulResolver provides a controllable SoulResolver for tests.
type MockSoulResolver struct {
	ResolveSoulFunc func(agentID string) (string, error)
}

// ResolveSoul delegates to ResolveSoulFunc if set, otherwise returns the default prompt.
func (m *MockSoulResolver) ResolveSoul(agentID string) (string, error) {
	if m.ResolveSoulFunc != nil {
		return m.ResolveSoulFunc(agentID)
	}
	return "You are a helpful assistant.", nil
}

// Interface guards.
var (
	_ router.ResponseSender  = (*MockResponseSender)(nil)
	_ router.AgentFactory    = (*MockAgentFactory)(nil)
	_ router.SessionStore    = (*MockSessionStore)(nil)
	_ router.HistoryResolver = (*MockHistoryResolver)(nil)
	_ router.SoulResolver    = (*MockSoulResolver)(nil)
)
