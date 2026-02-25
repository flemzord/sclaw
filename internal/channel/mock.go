package channel

import (
	"context"
	"sync"

	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/pkg/message"
)

// MockChannel is a test double that implements Channel. It records sent
// messages and allows simulating inbound messages via SimulateMessage.
type MockChannel struct {
	name      string
	inbox     func(msg message.InboundMessage) error
	mu        sync.Mutex
	sent      []message.OutboundMessage
	allowList *AllowList

	// SendFunc, if set, is called instead of the default recording behavior.
	SendFunc func(ctx context.Context, msg message.OutboundMessage) error
}

// Compile-time interface guards.
var _ Channel = (*MockChannel)(nil)

// NewMockChannel creates a MockChannel with the given name and an optional
// allow-list. Pass nil for allowList to deny all messages (security by default).
func NewMockChannel(name string, allowList *AllowList) *MockChannel {
	return &MockChannel{
		name:      name,
		allowList: allowList,
	}
}

// ModuleInfo implements core.Module.
func (m *MockChannel) ModuleInfo() core.ModuleInfo {
	return core.ModuleInfo{
		ID: core.ModuleID("channel." + m.name),
		New: func() core.Module {
			return NewMockChannel(m.name, m.allowList)
		},
	}
}

// Send records the outbound message. If SendFunc is set, it delegates to it.
func (m *MockChannel) Send(ctx context.Context, msg message.OutboundMessage) error {
	if m.SendFunc != nil {
		return m.SendFunc(ctx, msg)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

// SetInbox stores the inbox callback provided by the router.
func (m *MockChannel) SetInbox(fn func(msg message.InboundMessage) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inbox = fn
}

// SimulateMessage pushes an inbound message through the allow-list and into
// the inbox. It returns ErrDenied if the sender is not allowed, and ErrNoInbox
// if SetInbox has not been called.
func (m *MockChannel) SimulateMessage(msg message.InboundMessage) error {
	m.mu.Lock()
	al := m.allowList
	inbox := m.inbox
	m.mu.Unlock()

	if !al.IsAllowed(msg) {
		return ErrDenied
	}
	if inbox == nil {
		return ErrNoInbox
	}

	// Tag the message with this channel's name.
	msg.Channel = m.name
	return inbox(msg)
}

// SentMessages returns a copy of all outbound messages recorded by Send.
func (m *MockChannel) SentMessages() []message.OutboundMessage {
	m.mu.Lock()
	defer m.mu.Unlock()

	cp := make([]message.OutboundMessage, len(m.sent))
	copy(cp, m.sent)
	return cp
}

// Reset clears recorded sent messages.
func (m *MockChannel) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = nil
}

// MockStreamingChannel extends MockChannel with streaming and typing support.
type MockStreamingChannel struct {
	*MockChannel

	mu           sync.Mutex
	streaming    bool
	typingChats  []message.Chat
	streamChunks []string

	// SupportsStreamingFunc overrides the default SupportsStreaming behavior.
	SupportsStreamingFunc func() bool
}

// Compile-time interface guards.
var (
	_ StreamingChannel = (*MockStreamingChannel)(nil)
	_ TypingChannel    = (*MockStreamingChannel)(nil)
)

// NewMockStreamingChannel creates a MockStreamingChannel.
func NewMockStreamingChannel(name string, allowList *AllowList) *MockStreamingChannel {
	return &MockStreamingChannel{
		MockChannel: NewMockChannel(name, allowList),
		streaming:   true,
	}
}

// SupportsStreaming implements StreamingChannel.
func (m *MockStreamingChannel) SupportsStreaming() bool {
	if m.SupportsStreamingFunc != nil {
		return m.SupportsStreamingFunc()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.streaming
}

// SendStream implements StreamingChannel. It collects all chunks.
func (m *MockStreamingChannel) SendStream(_ context.Context, _ message.Chat, stream <-chan string) error {
	for chunk := range stream {
		m.mu.Lock()
		m.streamChunks = append(m.streamChunks, chunk)
		m.mu.Unlock()
	}
	return nil
}

// SendTyping implements TypingChannel. It records the chat.
func (m *MockStreamingChannel) SendTyping(_ context.Context, chat message.Chat) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.typingChats = append(m.typingChats, chat)
	return nil
}

// StreamChunks returns a copy of all stream chunks received.
func (m *MockStreamingChannel) StreamChunks() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	cp := make([]string, len(m.streamChunks))
	copy(cp, m.streamChunks)
	return cp
}

// TypingChats returns a copy of all chats that received typing indicators.
func (m *MockStreamingChannel) TypingChats() []message.Chat {
	m.mu.Lock()
	defer m.mu.Unlock()

	cp := make([]message.Chat, len(m.typingChats))
	copy(cp, m.typingChats)
	return cp
}
