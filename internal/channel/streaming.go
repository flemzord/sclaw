package channel

import (
	"context"
	"time"

	"github.com/flemzord/sclaw/pkg/message"
)

const defaultTypingInterval = time.Second

// StreamingChannel is implemented by channels that support streaming partial
// responses to the user as they are generated.
type StreamingChannel interface {
	Channel

	// SupportsStreaming reports whether this channel currently supports streaming.
	// A channel may dynamically disable streaming (e.g., platform rate limit).
	SupportsStreaming() bool

	// SendStream delivers a stream of text chunks to the platform.
	// The channel should aggregate chunks and flush periodically.
	// The stream is closed by the caller when the response is complete.
	SendStream(ctx context.Context, chat message.Chat, stream <-chan string) error
}

// TypingChannel is implemented by channels that can show typing indicators
// to the user while the agent is processing.
type TypingChannel interface {
	Channel

	// SendTyping sends a single typing indicator to the platform.
	SendTyping(ctx context.Context, chat message.Chat) error
}

// StartTypingLoop launches a goroutine that sends typing indicators at the
// given interval until the context is cancelled. It is safe to call from
// multiple goroutines; the loop stops when ctx is done.
func StartTypingLoop(ctx context.Context, ch TypingChannel, chat message.Chat, interval time.Duration) {
	if interval <= 0 {
		interval = defaultTypingInterval
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		const maxConsecutiveErrors = 3
		var consecutiveErrors int

		// Send an initial typing indicator immediately.
		if err := ch.SendTyping(ctx, chat); err != nil {
			consecutiveErrors++
			if consecutiveErrors >= maxConsecutiveErrors {
				return
			}
		} else {
			consecutiveErrors = 0
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := ch.SendTyping(ctx, chat); err != nil {
					consecutiveErrors++
					if consecutiveErrors >= maxConsecutiveErrors {
						return
					}
					continue
				}
				consecutiveErrors = 0
			}
		}
	}()
}
