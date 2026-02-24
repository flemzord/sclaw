package message

import (
	"encoding/json"
	"testing"
)

func TestNewTextMessage(t *testing.T) {
	chat := Chat{ID: "chat-1", Type: ChatDM}
	m := NewTextMessage(chat, "hello")

	if m.Chat.ID != "chat-1" {
		t.Errorf("Chat.ID = %q, want %q", m.Chat.ID, "chat-1")
	}
	if len(m.Blocks) != 1 {
		t.Fatalf("len(Blocks) = %d, want 1", len(m.Blocks))
	}
	if m.Blocks[0].Type != BlockText {
		t.Errorf("Blocks[0].Type = %q, want %q", m.Blocks[0].Type, BlockText)
	}
	if m.Blocks[0].Text != "hello" {
		t.Errorf("Blocks[0].Text = %q, want %q", m.Blocks[0].Text, "hello")
	}
}

func TestOutboundMessage_TextContent(t *testing.T) {
	m := NewTextMessage(Chat{ID: "1", Type: ChatDM}, "hello")
	if got := m.TextContent(); got != "hello" {
		t.Errorf("TextContent() = %q, want %q", got, "hello")
	}
}

func TestOutboundMessage_HasMedia(t *testing.T) {
	m := NewTextMessage(Chat{ID: "1", Type: ChatDM}, "hello")
	if m.HasMedia() {
		t.Error("HasMedia() = true for text-only message")
	}

	m.Blocks = append(m.Blocks, NewImageBlock("url", "image/png"))
	if !m.HasMedia() {
		t.Error("HasMedia() = false after adding image block")
	}
}

func TestOutboundHints_ZeroValue(t *testing.T) {
	var h OutboundHints
	if h.DisablePreview {
		t.Error("DisablePreview should be false by default")
	}
	if h.DisableNotification {
		t.Error("DisableNotification should be false by default")
	}
	if h.ParseMode != "" {
		t.Errorf("ParseMode = %q, want empty", h.ParseMode)
	}
}

func TestOutboundHints_OmittedInJSON(t *testing.T) {
	m := NewTextMessage(Chat{ID: "1", Type: ChatDM}, "hello")
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if _, ok := raw["hints"]; ok {
		t.Error("hints should be omitted from JSON when zero value")
	}
}

func TestOutboundMessage_JSONRoundTrip(t *testing.T) {
	original := OutboundMessage{
		Chat:      Chat{ID: "chat-1", Type: ChatGroup, Title: "General"},
		ThreadID:  "thread-1",
		ReplyToID: "msg-50",
		Blocks: []ContentBlock{
			NewTextBlock("Reply with image"),
			NewImageBlock("https://example.com/img.png", "image/png"),
		},
		Hints: &OutboundHints{
			DisablePreview: true,
			ParseMode:      "markdown",
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded OutboundMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.Chat.ID != original.Chat.ID {
		t.Errorf("Chat.ID = %q, want %q", decoded.Chat.ID, original.Chat.ID)
	}
	if decoded.ThreadID != original.ThreadID {
		t.Errorf("ThreadID = %q, want %q", decoded.ThreadID, original.ThreadID)
	}
	if len(decoded.Blocks) != 2 {
		t.Fatalf("len(Blocks) = %d, want 2", len(decoded.Blocks))
	}
	if decoded.Blocks[0].Text != "Reply with image" {
		t.Errorf("Blocks[0].Text = %q, want %q", decoded.Blocks[0].Text, "Reply with image")
	}
	if decoded.Hints == nil {
		t.Fatal("Hints = nil, want non-nil")
	}
	if !decoded.Hints.DisablePreview {
		t.Error("Hints.DisablePreview = false, want true")
	}
	if decoded.Hints.ParseMode != "markdown" {
		t.Errorf("Hints.ParseMode = %q, want %q", decoded.Hints.ParseMode, "markdown")
	}
}
