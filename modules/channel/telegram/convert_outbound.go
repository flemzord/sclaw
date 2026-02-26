package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/flemzord/sclaw/internal/channel"
	"github.com/flemzord/sclaw/pkg/message"
)

// sendOutbound sends an OutboundMessage through the Telegram API.
// It splits the message if needed and dispatches each block by type.
// The Telegram struct is defined in telegram.go (Phase 4).
func (t *Telegram) sendOutbound(ctx context.Context, msg message.OutboundMessage) error {
	chunks := channel.SplitMessage(msg, channel.ChunkConfig{
		MaxLength:      t.config.MaxMessageLength,
		PreserveBlocks: true,
	})

	chatID, err := strconv.ParseInt(msg.Chat.ID, 10, 64)
	if err != nil {
		return fmt.Errorf("telegram: invalid chat ID %q: %w", msg.Chat.ID, err)
	}

	for _, chunk := range chunks {
		if err := t.sendChunk(ctx, chunk, chatID); err != nil {
			return err
		}
	}

	return nil
}

// sendChunk dispatches a single chunk's blocks to the appropriate Telegram API methods.
// Design: fail-fast — if any block send fails, remaining blocks are skipped and the
// error is returned immediately. This prevents partial delivery from being silently
// treated as success by the caller.
func (t *Telegram) sendChunk(ctx context.Context, chunk message.OutboundMessage, chatID int64) error {
	threadID := parseOptionalInt(chunk.ThreadID, t.logger)
	replyToID := parseOptionalInt(chunk.ReplyToID, t.logger)
	parseMode := resolveParseMode(chunk.Hints)
	disablePreview := false
	disableNotification := false

	if chunk.Hints != nil {
		disablePreview = chunk.Hints.DisablePreview
		disableNotification = chunk.Hints.DisableNotification
	}

	for _, block := range chunk.Blocks {
		var err error

		switch block.Type {
		case message.BlockText:
			text := block.Text
			pm := parseMode
			if pm == "" {
				text = FormatMarkdownV2(text)
				pm = "MarkdownV2"
			}
			_, err = t.client.SendMessage(ctx, SendMessageRequest{
				ChatID:                chatID,
				Text:                  text,
				ParseMode:             pm,
				MessageThreadID:       threadID,
				ReplyToMessageID:      replyToID,
				DisableWebPagePreview: disablePreview,
				DisableNotification:   disableNotification,
			})

		case message.BlockImage:
			caption := block.Caption
			pm := parseMode
			if pm == "" && caption != "" {
				caption = FormatMarkdownV2(caption)
				pm = "MarkdownV2"
			}
			_, err = t.client.SendPhoto(ctx, SendPhotoRequest{
				ChatID:              chatID,
				Photo:               block.URL,
				Caption:             caption,
				ParseMode:           pm,
				MessageThreadID:     threadID,
				ReplyToMessageID:    replyToID,
				DisableNotification: disableNotification,
			})

		case message.BlockAudio:
			caption := block.Caption
			pm := parseMode
			if pm == "" && caption != "" {
				caption = FormatMarkdownV2(caption)
				pm = "MarkdownV2"
			}
			if block.IsVoice {
				_, err = t.client.SendVoice(ctx, SendVoiceRequest{
					ChatID:              chatID,
					Voice:               block.URL,
					Caption:             caption,
					ParseMode:           pm,
					MessageThreadID:     threadID,
					ReplyToMessageID:    replyToID,
					DisableNotification: disableNotification,
				})
			} else {
				_, err = t.client.SendAudio(ctx, SendAudioRequest{
					ChatID:              chatID,
					Audio:               block.URL,
					Caption:             caption,
					ParseMode:           pm,
					MessageThreadID:     threadID,
					ReplyToMessageID:    replyToID,
					DisableNotification: disableNotification,
				})
			}

		case message.BlockFile:
			caption := block.Caption
			pm := parseMode
			if pm == "" && caption != "" {
				caption = FormatMarkdownV2(caption)
				pm = "MarkdownV2"
			}
			_, err = t.client.SendDocument(ctx, SendDocumentRequest{
				ChatID:              chatID,
				Document:            block.URL,
				Caption:             caption,
				ParseMode:           pm,
				MessageThreadID:     threadID,
				ReplyToMessageID:    replyToID,
				DisableNotification: disableNotification,
			})

		case message.BlockLocation:
			if block.Lat == nil || block.Lon == nil {
				t.logger.Warn("skipping location block with nil coordinates",
					"chat_id", chatID,
					"has_lat", block.Lat != nil,
					"has_lon", block.Lon != nil,
				)
				continue
			}
			_, err = t.client.SendLocation(ctx, SendLocationRequest{
				ChatID:              chatID,
				Latitude:            *block.Lat,
				Longitude:           *block.Lon,
				MessageThreadID:     threadID,
				ReplyToMessageID:    replyToID,
				DisableNotification: disableNotification,
			})

		default:
			// Skip unsupported block types (BlockRaw, BlockReaction, etc.).
			continue
		}

		if err != nil {
			return fmt.Errorf("telegram: send %s block: %w", block.Type, err)
		}
	}

	return nil
}

// resolveParseMode returns the parse mode from hints.
// Returns empty string if no parse mode is specified, which tells Telegram
// to treat the text as plain text (no special formatting).
func resolveParseMode(hints *message.OutboundHints) string {
	if hints != nil && hints.ParseMode != "" {
		return hints.ParseMode
	}
	return ""
}

// parseOptionalInt converts a string to int, returning 0 for empty strings.
// Logs a warning if the string is non-empty but not a valid integer.
func parseOptionalInt(s string, logger *slog.Logger) int {
	if s == "" {
		return 0
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		logger.Warn("parseOptionalInt: invalid integer value",
			"value", s,
			"error", err,
		)
		return 0
	}
	return v
}
