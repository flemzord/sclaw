package channel

import (
	"context"
	"testing"
	"time"

	"github.com/flemzord/sclaw/pkg/message"
)

func TestStartTypingLoop_NonPositiveIntervalDoesNotPanic(t *testing.T) {
	t.Parallel()

	ch := NewMockStreamingChannel("test", NewAllowList([]string{"alice"}, nil))
	chat := message.Chat{ID: "chat-1", Type: message.ChatDM}

	ctx, cancel := context.WithCancel(context.Background())
	StartTypingLoop(ctx, ch, chat, 0)

	time.Sleep(20 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	if len(ch.TypingChats()) == 0 {
		t.Fatal("expected at least one typing indicator")
	}
}
