package router

import (
	"context"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/pkg/message"
)

// ResponseSender delivers outbound messages to a channel.
type ResponseSender interface {
	Send(ctx context.Context, msg message.OutboundMessage) error
}

// AgentFactory creates agent loops for sessions.
type AgentFactory interface {
	ForSession(session *Session) (*agent.Loop, error)
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
