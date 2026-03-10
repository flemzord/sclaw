// Package channel defines the bridge between messaging platforms and the router.
// It provides the Channel interface, streaming support, typing indicators,
// message chunking, command registration, and allow-list filtering.
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
// Channels may optionally implement StreamingChannel, TypingChannel, or
// CommandRegistrar for richer interactions.
type Channel interface {
	core.Module

	// Send delivers an outbound message to the platform.
	Send(ctx context.Context, msg message.OutboundMessage) error

	// SetInbox gives the channel a function to push inbound messages to the router.
	// The router calls this during wiring, before Start().
	SetInbox(fn func(msg message.InboundMessage) error)
}

// BotCommand represents a slash command exposed to a messaging platform.
// Channels that support command autocomplete (e.g. Telegram, Discord) use
// these to register available commands with the platform's API.
type BotCommand struct {
	Command     string // command name without leading slash (e.g. "weather")
	Description string // short description shown in autocomplete
}

// CommandRegistrar is implemented by channels that support registering
// bot commands with the platform (e.g. Telegram's setMyCommands).
// This enables slash-command autocomplete in the chat UI.
type CommandRegistrar interface {
	Channel

	// RegisterCommands sets the list of available bot commands on the platform.
	// An empty slice clears all commands.
	RegisterCommands(ctx context.Context, commands []BotCommand) error
}
