// Package channel defines the bridge between messaging platforms and the router.
// It provides the Channel interface, streaming support, typing indicators,
// message chunking, and allow-list filtering.
package channel

import (
	"context"

	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/pkg/message"
)

// Channel is the bridge between a messaging platform and the router.
// Every concrete channel (Telegram, Discord, etc.) must implement this interface.
//
// A channel receives messages from its platform, checks the allow-list, and pushes
// them to the router via the inbox callback. It also receives outbound messages
// from the router via Send().
//
// Channels may optionally implement StreamingChannel or TypingChannel for
// richer interactions.
type Channel interface {
	core.Module

	// Send delivers an outbound message to the platform.
	Send(ctx context.Context, msg message.OutboundMessage) error

	// SetInbox gives the channel a function to push inbound messages to the router.
	// The router calls this during wiring, before Start().
	SetInbox(fn func(msg message.InboundMessage) error)
}
