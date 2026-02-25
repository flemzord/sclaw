package router

import (
	"testing"

	"github.com/flemzord/sclaw/pkg/message"
)

func TestSessionKeyFromMessage(t *testing.T) {
	t.Parallel()

	msg := message.InboundMessage{
		Channel:  "slack",
		Chat:     message.Chat{ID: "C123"},
		ThreadID: "T456",
	}

	key := SessionKeyFromMessage(msg)

	if key.Channel != "slack" {
		t.Errorf("Channel = %q, want %q", key.Channel, "slack")
	}
	if key.ChatID != "C123" {
		t.Errorf("ChatID = %q, want %q", key.ChatID, "C123")
	}
	if key.ThreadID != "T456" {
		t.Errorf("ThreadID = %q, want %q", key.ThreadID, "T456")
	}
}

func TestSessionKeyFromMessage_EmptyThread(t *testing.T) {
	t.Parallel()

	msg := message.InboundMessage{
		Channel: "telegram",
		Chat:    message.Chat{ID: "G789"},
		// ThreadID intentionally left empty.
	}

	key := SessionKeyFromMessage(msg)

	if key.Channel != "telegram" {
		t.Errorf("Channel = %q, want %q", key.Channel, "telegram")
	}
	if key.ChatID != "G789" {
		t.Errorf("ChatID = %q, want %q", key.ChatID, "G789")
	}
	if key.ThreadID != "" {
		t.Errorf("ThreadID = %q, want empty string", key.ThreadID)
	}
}

func TestSessionKeyEquality(t *testing.T) {
	t.Parallel()

	k1 := SessionKey{Channel: "slack", ChatID: "C1", ThreadID: "T1"}
	k2 := SessionKey{Channel: "slack", ChatID: "C1", ThreadID: "T1"}
	k3 := SessionKey{Channel: "slack", ChatID: "C1", ThreadID: "T2"}

	if k1 != k2 {
		t.Error("identical keys should be equal")
	}
	if k1 == k3 {
		t.Error("keys with different ThreadID should not be equal")
	}
}
