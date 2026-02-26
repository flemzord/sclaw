// Package telegram implements the Telegram Bot API channel for sclaw.
//
// It provides a bidirectional bridge between Telegram and sclaw's
// platform-agnostic message model, supporting:
//
//   - Inbound message conversion (text, photo, audio, voice, document, location, sticker)
//   - Outbound message dispatch with automatic chunking via channel.SplitMessage
//   - Two delivery modes: long-polling (default) and webhook
//   - Streaming responses via editMessageText with configurable flush interval
//   - Typing indicators via sendChatAction
//   - MarkdownV2 escaping and formatting utilities
//
// The module registers itself as "channel.telegram" via init() and implements
// the full sclaw module lifecycle: Configure → Provision → Validate → Start → Stop.
//
// No external Telegram library is used — the module communicates with the
// Bot API via raw net/http + encoding/json.
package telegram
