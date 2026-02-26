package telegram

import (
	"context"
	"fmt"
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
func (t *Telegram) sendChunk(ctx context.Context, chunk message.OutboundMessage, chatID int64) error {
	threadID := parseOptionalInt(chunk.ThreadID)
	replyToID := parseOptionalInt(chunk.ReplyToID)
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
			_, err = t.client.SendMessage(ctx, SendMessageRequest{
				ChatID:                chatID,
				Text:                  block.Text,
				ParseMode:             parseMode,
				MessageThreadID:       threadID,
				ReplyToMessageID:      replyToID,
				DisableWebPagePreview: disablePreview,
				DisableNotification:   disableNotification,
			})

		case message.BlockImage:
			_, err = t.client.SendPhoto(ctx, SendPhotoRequest{
				ChatID:              chatID,
				Photo:               block.URL,
				Caption:             block.Caption,
				ParseMode:           parseMode,
				MessageThreadID:     threadID,
				ReplyToMessageID:    replyToID,
				DisableNotification: disableNotification,
			})

		case message.BlockAudio:
			if block.IsVoice {
				_, err = t.client.SendVoice(ctx, SendVoiceRequest{
					ChatID:              chatID,
					Voice:               block.URL,
					Caption:             block.Caption,
					ParseMode:           parseMode,
					MessageThreadID:     threadID,
					ReplyToMessageID:    replyToID,
					DisableNotification: disableNotification,
				})
			} else {
				_, err = t.client.SendAudio(ctx, SendAudioRequest{
					ChatID:              chatID,
					Audio:               block.URL,
					Caption:             block.Caption,
					ParseMode:           parseMode,
					MessageThreadID:     threadID,
					ReplyToMessageID:    replyToID,
					DisableNotification: disableNotification,
				})
			}

		case message.BlockFile:
			_, err = t.client.SendDocument(ctx, SendDocumentRequest{
				ChatID:              chatID,
				Document:            block.URL,
				Caption:             block.Caption,
				ParseMode:           parseMode,
				MessageThreadID:     threadID,
				ReplyToMessageID:    replyToID,
				DisableNotification: disableNotification,
			})

		case message.BlockLocation:
			lat := 0.0
			lon := 0.0
			if block.Lat != nil {
				lat = *block.Lat
			}
			if block.Lon != nil {
				lon = *block.Lon
			}
			_, err = t.client.SendLocation(ctx, SendLocationRequest{
				ChatID:              chatID,
				Latitude:            lat,
				Longitude:           lon,
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
func parseOptionalInt(s string) int {
	if s == "" {
		return 0
	}
	v, _ := strconv.Atoi(s)
	return v
}
