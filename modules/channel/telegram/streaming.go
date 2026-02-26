package telegram

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/flemzord/sclaw/pkg/message"
)

const streamPlaceholder = "\u2026" // Ellipsis character

// minFlushDelta is the minimum character delta before flushing an edit.
const minFlushDelta = 200

// SupportsStreaming reports whether the Telegram channel currently supports
// streaming. It may return false if repeated streaming errors have been
// detected, disabling streaming until the next successful Send.
func (t *Telegram) SupportsStreaming() bool {
	return !t.streamingDisabled.Load()
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
	overflow := false
	var consecutiveFlushErrors int
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
		// Truncate on a valid UTF-8 rune boundary.
		if len(text) > maxLen {
			text = truncateUTF8(text, maxLen)
		}
		_, editErr := t.client.EditMessageText(ctx, EditMessageTextRequest{
			ChatID:    chatID,
			MessageID: placeholder.MessageID,
			Text:      text,
		})
		if editErr != nil {
			var apiErr *APIError
			if errors.As(editErr, &apiErr) {
				// Ignore "message is not modified" errors.
				if apiErr.Code == 400 && strings.Contains(apiErr.Description, "not modified") {
					return
				}
				// Respect rate limits — wait then retry once.
				if apiErr.RetryAfter > 0 {
					timer := time.NewTimer(time.Duration(apiErr.RetryAfter) * time.Second)
					select {
					case <-ctx.Done():
						timer.Stop()
						return
					case <-timer.C:
					}
					// Retry the edit after waiting.
					_, retryErr := t.client.EditMessageText(ctx, EditMessageTextRequest{
						ChatID:    chatID,
						MessageID: placeholder.MessageID,
						Text:      text,
					})
					if retryErr == nil {
						lastFlushed = len(text)
						consecutiveFlushErrors = 0
					}
					return
				}
			}
			// Log non-API errors instead of silently swallowing them.
			consecutiveFlushErrors++
			t.logger.Warn("streaming edit failed",
				"error", editErr,
				"chat_id", chatID,
				"consecutive_errors", consecutiveFlushErrors,
			)
			// Disable streaming after 5 consecutive flush errors.
			if consecutiveFlushErrors >= 5 {
				t.streamingDisabled.Store(true)
				t.logger.Warn("streaming disabled due to repeated errors",
					"chat_id", chatID,
				)
			}
			return
		}
		lastFlushed = len(text)
		consecutiveFlushErrors = 0
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return ctx.Err()

		case chunk, ok := <-stream:
			if !ok {
				flush()
				return nil
			}

			if overflow {
				// Buffer is full — drain remaining chunks without writing.
				continue
			}

			buf.WriteString(chunk)

			// Check if buffer exceeds max length.
			if buf.Len() > maxLen {
				overflow = true
				t.logger.Warn("streaming message exceeded max length, truncating",
					"max_length", maxLen,
					"chat_id", chatID,
				)
				flush()
			} else if buf.Len()-lastFlushed >= minFlushDelta {
				flush()
			}

		case <-ticker.C:
			flush()
		}
	}
}

// truncateUTF8 truncates s to at most maxBytes, walking back to a valid
// UTF-8 rune boundary to avoid producing invalid UTF-8.
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}
