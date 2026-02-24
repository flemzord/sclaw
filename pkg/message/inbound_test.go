package message

import (
	"encoding/json"
	"testing"
	"time"
)

func TestInboundMessage_TextContent(t *testing.T) {
	tests := []struct {
		name   string
		blocks []ContentBlock
		want   string
	}{
		{"single text", []ContentBlock{NewTextBlock("hello")}, "hello"},
		{"multi-block text", []ContentBlock{NewTextBlock("a"), NewTextBlock("b")}, "a\nb"},
		{"mixed with media", []ContentBlock{
			NewTextBlock("caption"),
			NewImageBlock("url", "image/png"),
			NewTextBlock("more text"),
		}, "caption\nmore text"},
		{"no text", []ContentBlock{NewImageBlock("url", "image/png")}, ""},
		{"empty", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &InboundMessage{Blocks: tt.blocks}
			if got := m.TextContent(); got != tt.want {
				t.Errorf("TextContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInboundMessage_HasMedia(t *testing.T) {
	tests := []struct {
		name   string
		blocks []ContentBlock
		want   bool
	}{
		{"with image", []ContentBlock{NewTextBlock("hi"), NewImageBlock("url", "image/png")}, true},
		{"text only", []ContentBlock{NewTextBlock("hi")}, false},
		{"empty", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &InboundMessage{Blocks: tt.blocks}
			if got := m.HasMedia(); got != tt.want {
				t.Errorf("HasMedia() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInboundMessage_IsGroup(t *testing.T) {
	m := &InboundMessage{Chat: Chat{ID: "1", Type: ChatGroup}}
	if !m.IsGroup() {
		t.Error("IsGroup() = false, want true")
	}

	m2 := &InboundMessage{Chat: Chat{ID: "2", Type: ChatDM}}
	if m2.IsGroup() {
		t.Error("IsGroup() = true, want false")
	}
}

func TestInboundMessage_IsDirectMessage(t *testing.T) {
	m := &InboundMessage{Chat: Chat{ID: "1", Type: ChatDM}}
	if !m.IsDirectMessage() {
		t.Error("IsDirectMessage() = false, want true")
	}

	m2 := &InboundMessage{Chat: Chat{ID: "2", Type: ChatGroup}}
	if m2.IsDirectMessage() {
		t.Error("IsDirectMessage() = true, want false")
	}
}

func TestInboundMessage_JSONRoundTrip(t *testing.T) {
	ts := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	original := InboundMessage{
		ID:        "msg-123",
		Timestamp: ts,
		Channel:   "channel.telegram",
		Sender:    Sender{ID: "user-1", Username: "alice", DisplayName: "Alice"},
		Chat:      Chat{ID: "chat-1", Type: ChatGroup, Title: "Dev Team"},
		ThreadID:  "thread-1",
		ReplyToID: "msg-100",
		Blocks: []ContentBlock{
			NewTextBlock("Hello world"),
			NewImageBlock("https://example.com/img.png", "image/png"),
		},
		Mentions: &Mentions{
			IDs:         []string{"bot-1"},
			IsMentioned: true,
		},
		Raw: json.RawMessage(`{"update_id":12345}`),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded InboundMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if !decoded.Timestamp.Equal(original.Timestamp) {
		t.Errorf("Timestamp = %v, want %v", decoded.Timestamp, original.Timestamp)
	}
	if decoded.Channel != original.Channel {
		t.Errorf("Channel = %q, want %q", decoded.Channel, original.Channel)
	}
	if decoded.Sender.Username != original.Sender.Username {
		t.Errorf("Sender.Username = %q, want %q", decoded.Sender.Username, original.Sender.Username)
	}
	if decoded.Chat.Type != original.Chat.Type {
		t.Errorf("Chat.Type = %q, want %q", decoded.Chat.Type, original.Chat.Type)
	}
	if decoded.ThreadID != original.ThreadID {
		t.Errorf("ThreadID = %q, want %q", decoded.ThreadID, original.ThreadID)
	}
	if len(decoded.Blocks) != len(original.Blocks) {
		t.Fatalf("len(Blocks) = %d, want %d", len(decoded.Blocks), len(original.Blocks))
	}
	if decoded.Blocks[0].Text != "Hello world" {
		t.Errorf("Blocks[0].Text = %q, want %q", decoded.Blocks[0].Text, "Hello world")
	}
	if decoded.Blocks[1].Type != BlockImage {
		t.Errorf("Blocks[1].Type = %q, want %q", decoded.Blocks[1].Type, BlockImage)
	}
	if decoded.Mentions == nil {
		t.Fatal("Mentions = nil, want non-nil")
	}
	if !decoded.Mentions.IsMentioned {
		t.Error("Mentions.IsMentioned = false, want true")
	}
	if string(decoded.Raw) != `{"update_id":12345}` {
		t.Errorf("Raw = %s, want %s", decoded.Raw, `{"update_id":12345}`)
	}
}

func TestInboundMessage_MentionsOmittedInJSON(t *testing.T) {
	tests := []struct {
		name     string
		mentions *Mentions
	}{
		{"nil mentions", nil},
		{"empty non-nil mentions", &Mentions{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := InboundMessage{
				ID:       "msg-1",
				Channel:  "test",
				Chat:     Chat{ID: "1", Type: ChatDM},
				Blocks:   []ContentBlock{NewTextBlock("hi")},
				Mentions: tt.mentions,
			}
			data, err := json.Marshal(m)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			var raw map[string]json.RawMessage
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			if _, ok := raw["mentions"]; ok {
				t.Error("mentions should be omitted from JSON when empty")
			}
		})
	}
}

func TestInboundMessage_Multimodal(t *testing.T) {
	m := &InboundMessage{
		Blocks: []ContentBlock{
			NewTextBlock("Check this out"),
			NewImageBlock("https://example.com/photo.jpg", "image/jpeg"),
			NewAudioBlock("https://example.com/voice.ogg", "audio/ogg", true),
			NewLocationBlock(48.8566, 2.3522),
		},
	}

	if got := m.TextContent(); got != "Check this out" {
		t.Errorf("TextContent() = %q, want %q", got, "Check this out")
	}
	if !m.HasMedia() {
		t.Error("HasMedia() = false, want true for multimodal message")
	}
}
