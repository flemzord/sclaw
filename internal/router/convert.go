package router

import (
	"context"
	"time"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/hook"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/pkg/message"
)

// ResponseSender delivers outbound messages to a channel.
type ResponseSender interface {
	Send(ctx context.Context, msg message.OutboundMessage) error
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

// sessionViewAdapter wraps a *Session to implement hook.SessionView.
// This breaks the router→hook circular dependency by providing read-only
// session access without exposing the full Session struct.
type sessionViewAdapter struct{ s *Session }

// Compile-time interface check.
var _ hook.SessionView = (*sessionViewAdapter)(nil)

func (a *sessionViewAdapter) SessionID() string { return a.s.ID }

func (a *sessionViewAdapter) SessionKey() (string, string, string) {
	return a.s.Key.Channel, a.s.Key.ChatID, a.s.Key.ThreadID
}

func (a *sessionViewAdapter) AgentID() string      { return a.s.AgentID }
func (a *sessionViewAdapter) CreatedAt() time.Time { return a.s.CreatedAt }

func (a *sessionViewAdapter) GetMetadata(key string) (any, bool) {
	if a.s.Metadata == nil {
		return nil, false
	}
	v, ok := a.s.Metadata[key]
	return v, ok
}
