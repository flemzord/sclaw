package router

import (
	"context"
	"time"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/channel"
	"github.com/flemzord/sclaw/internal/hook"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/pkg/message"
)

// Compile-time check that sessionViewAdapter implements hook.SessionView.
var _ hook.SessionView = (*sessionViewAdapter)(nil)

// ResponseSender delivers outbound messages to a channel.
type ResponseSender interface {
	Send(ctx context.Context, msg message.OutboundMessage) error
}

// ChannelLookup resolves a channel by name. Implemented by channel.Dispatcher.
type ChannelLookup interface {
	Get(name string) (channel.Channel, bool)
}

// AgentFactory creates agent loops for sessions.
type AgentFactory interface {
	ForSession(session *Session, msg message.InboundMessage) (*agent.Loop, error)
}

// messageToLLM converts an inbound message to a user-role LLM message.
func messageToLLM(msg message.InboundMessage) provider.LLMMessage {
	return provider.LLMMessage{
		Role:    provider.MessageRoleUser,
		Content: msg.TextContent(),
	}
}

// buildOutbound creates an outbound text response preserving thread/reply context.
func buildOutbound(original message.InboundMessage, resp agent.Response) message.OutboundMessage {
	out := message.NewTextMessage(original.Chat, resp.Content)
	out.Channel = original.Channel
	out.ThreadID = original.ThreadID
	out.ReplyToID = original.ID
	return out
}

// sessionViewAdapter provides a read-only view of a Session for use by hooks.
// It exists to decouple hook implementations from the internal Session type.
type sessionViewAdapter struct {
	session *Session
}

// SessionID returns the session identifier.
func (a *sessionViewAdapter) SessionID() string {
	return a.session.ID
}

// SessionKey returns the channel, chatID, and threadID for the session.
func (a *sessionViewAdapter) SessionKey() (channel, chatID, threadID string) {
	return a.session.Key.Channel, a.session.Key.ChatID, a.session.Key.ThreadID
}

// AgentID returns the agent assigned to this session.
func (a *sessionViewAdapter) AgentID() string {
	return a.session.AgentID
}

// CreatedAt returns when the session was created.
func (a *sessionViewAdapter) CreatedAt() time.Time {
	return a.session.CreatedAt
}

// GetMetadata returns a shallow copy of a single metadata value to prevent
// callers from mutating the session's internal state (m-30 fix).
func (a *sessionViewAdapter) GetMetadata(key string) (any, bool) {
	if a.session.Metadata == nil {
		return nil, false
	}
	v, ok := a.session.Metadata[key]
	return v, ok
}
