package telegram

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/flemzord/sclaw/pkg/message"
)

const streamPlaceholder = "\u2026" // Ellipsis character

// minFlushDelta is the minimum character delta before flushing an edit.
const minFlushDelta = 200

// SupportsStreaming reports that the Telegram channel supports streaming.
func (t *Telegram) SupportsStreaming() bool {
	return true
}

// SendStream delivers a stream of text chunks by editing a placeholder message.
func (t *Telegram) SendStream(ctx context.Context, chat message.Chat, stream <-chan string) error {
	chatID, err := strconv.ParseInt(chat.ID, 10, 64)
	if err != nil {
		return errors.New("telegram: invalid chat ID: " + chat.ID)
	}

	// Send initial placeholder message.
	placeholder, err := t.client.SendMessage(ctx, SendMessageRequest{
		ChatID: chatID,
		Text:   streamPlaceholder,
	})
	if err != nil {
		return err
	}

	var buf strings.Builder
	lastFlushed := 0
	maxLen := t.config.MaxMessageLength
	if maxLen <= 0 {
		maxLen = 4096
	}

	ticker := time.NewTicker(t.config.StreamFlushInterval)
	defer ticker.Stop()

	flush := func() {
		text := buf.String()
		if len(text) == lastFlushed {
			return
		}
		// Telegram limits message text to MaxMessageLength characters.
		if len(text) > maxLen {
			text = text[:maxLen]
		}
		_, editErr := t.client.EditMessageText(ctx, EditMessageTextRequest{
			ChatID:    chatID,
			MessageID: placeholder.MessageID,
			Text:      text,
		})
		if editErr != nil {
			// Ignore "message is not modified" errors.
			var apiErr *APIError
			if errors.As(editErr, &apiErr) {
				if apiErr.Code == 400 && strings.Contains(apiErr.Description, "not modified") {
					return
				}
				// Respect rate limits.
				if apiErr.RetryAfter > 0 {
					timer := time.NewTimer(time.Duration(apiErr.RetryAfter) * time.Second)
					select {
					case <-ctx.Done():
						timer.Stop()
					case <-timer.C:
					}
					return
				}
			}
			// Log but don't fail — best effort streaming.
			return
		}
		lastFlushed = len(text)
	}

	for {
		select {
		case <-ctx.Done():
			// Final flush before exit.
			flush()
			return ctx.Err()

		case chunk, ok := <-stream:
			if !ok {
				// Stream closed — final flush.
				flush()
				return nil
			}
			buf.WriteString(chunk)

			// Cap buffer to prevent unbounded growth.
			if buf.Len() > maxLen {
				flush()
			} else if buf.Len()-lastFlushed >= minFlushDelta {
				flush()
			}

		case <-ticker.C:
			flush()
		}
	}
}
