package telegram

import (
	"encoding/json"
	"testing"

	"github.com/flemzord/sclaw/pkg/message"
)

func TestConvertInbound_TextMessage(t *testing.T) {
	update := &Update{
		UpdateID: 1,
		Message: &Message{
			MessageID: 42,
			From:      &User{ID: 123, FirstName: "John", LastName: "Doe", Username: "johndoe"},
			Chat:      Chat{ID: 456, Type: "private"},
			Date:      1700000000,
			Text:      "Hello, world!",
		},
	}

	inbound, err := convertInbound(update, "mybot", "telegram")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inbound.ID != "42" {
		t.Errorf("ID = %q, want %q", inbound.ID, "42")
	}
	if inbound.Channel != "telegram" {
		t.Errorf("Channel = %q, want %q", inbound.Channel, "telegram")
	}
	if inbound.Sender.ID != "123" {
		t.Errorf("Sender.ID = %q, want %q", inbound.Sender.ID, "123")
	}
	if inbound.Sender.Username != "johndoe" {
		t.Errorf("Sender.Username = %q, want %q", inbound.Sender.Username, "johndoe")
	}
	if inbound.Sender.DisplayName != "John Doe" {
		t.Errorf("Sender.DisplayName = %q, want %q", inbound.Sender.DisplayName, "John Doe")
	}
	if inbound.Chat.Type != message.ChatDM {
		t.Errorf("Chat.Type = %q, want %q", inbound.Chat.Type, message.ChatDM)
	}

	if len(inbound.Blocks) != 1 {
		t.Fatalf("len(Blocks) = %d, want 1", len(inbound.Blocks))
	}
	if inbound.Blocks[0].Type != message.BlockText {
		t.Errorf("Block.Type = %q, want %q", inbound.Blocks[0].Type, message.BlockText)
	}
	if inbound.Blocks[0].Text != "Hello, world!" {
		t.Errorf("Block.Text = %q, want %q", inbound.Blocks[0].Text, "Hello, world!")
	}
	if inbound.Raw == nil {
		t.Error("Raw should not be nil")
	}
}

func TestConvertInbound_PhotoMessage(t *testing.T) {
	update := &Update{
		UpdateID: 2,
		Message: &Message{
			MessageID: 43,
			From:      &User{ID: 123, FirstName: "Jane"},
			Chat:      Chat{ID: 456, Type: "group", Title: "Test Group"},
			Date:      1700000001,
			Photo: []PhotoSize{
				{FileID: "small", Width: 90, Height: 90},
				{FileID: "medium", Width: 320, Height: 320},
				{FileID: "large", Width: 800, Height: 800},
			},
			Caption: "Nice photo!",
		},
	}

	inbound, err := convertInbound(update, "mybot", "telegram")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(inbound.Blocks) != 2 {
		t.Fatalf("len(Blocks) = %d, want 2", len(inbound.Blocks))
	}

	// First block: image (largest size).
	img := inbound.Blocks[0]
	if img.Type != message.BlockImage {
		t.Errorf("Block[0].Type = %q, want %q", img.Type, message.BlockImage)
	}
	if img.URL != "tg://file_id/large" {
		t.Errorf("Block[0].URL = %q, want largest photo URL", img.URL)
	}

	// Second block: caption text.
	caption := inbound.Blocks[1]
	if caption.Type != message.BlockText {
		t.Errorf("Block[1].Type = %q, want %q", caption.Type, message.BlockText)
	}
	if caption.Text != "Nice photo!" {
		t.Errorf("Block[1].Text = %q, want %q", caption.Text, "Nice photo!")
	}
}

func TestConvertInbound_AudioMessage(t *testing.T) {
	update := &Update{
		UpdateID: 3,
		Message: &Message{
			MessageID: 44,
			From:      &User{ID: 123, FirstName: "John"},
			Chat:      Chat{ID: 456, Type: "private"},
			Date:      1700000002,
			Audio: &Audio{
				FileID:   "audio123",
				MIMEType: "audio/mpeg",
				Duration: 180,
			},
		},
	}

	inbound, err := convertInbound(update, "mybot", "telegram")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(inbound.Blocks) != 1 {
		t.Fatalf("len(Blocks) = %d, want 1", len(inbound.Blocks))
	}

	block := inbound.Blocks[0]
	if block.Type != message.BlockAudio {
		t.Errorf("Block.Type = %q, want %q", block.Type, message.BlockAudio)
	}
	if block.URL != "tg://file_id/audio123" {
		t.Errorf("Block.URL = %q, want correct audio URL", block.URL)
	}
	if block.MIMEType != "audio/mpeg" {
		t.Errorf("Block.MIMEType = %q, want %q", block.MIMEType, "audio/mpeg")
	}
	if block.IsVoice {
		t.Error("Block.IsVoice = true, want false for audio")
	}
}

func TestConvertInbound_VoiceMessage(t *testing.T) {
	update := &Update{
		UpdateID: 4,
		Message: &Message{
			MessageID: 45,
			From:      &User{ID: 123, FirstName: "John"},
			Chat:      Chat{ID: 456, Type: "private"},
			Date:      1700000003,
			Voice: &Voice{
				FileID:   "voice456",
				MIMEType: "audio/ogg",
				Duration: 5,
			},
		},
	}

	inbound, err := convertInbound(update, "mybot", "telegram")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(inbound.Blocks) != 1 {
		t.Fatalf("len(Blocks) = %d, want 1", len(inbound.Blocks))
	}

	block := inbound.Blocks[0]
	if block.Type != message.BlockAudio {
		t.Errorf("Block.Type = %q, want %q", block.Type, message.BlockAudio)
	}
	if block.URL != "tg://file_id/voice456" {
		t.Errorf("Block.URL = %q, want correct voice URL", block.URL)
	}
	if !block.IsVoice {
		t.Error("Block.IsVoice = false, want true for voice")
	}
}

func TestConvertInbound_DocumentMessage(t *testing.T) {
	update := &Update{
		UpdateID: 5,
		Message: &Message{
			MessageID: 46,
			From:      &User{ID: 123, FirstName: "John"},
			Chat:      Chat{ID: 456, Type: "private"},
			Date:      1700000004,
			Document: &Document{
				FileID:   "doc789",
				FileName: "report.pdf",
				MIMEType: "application/pdf",
			},
			Caption: "Here is the report",
		},
	}

	inbound, err := convertInbound(update, "mybot", "telegram")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(inbound.Blocks) != 2 {
		t.Fatalf("len(Blocks) = %d, want 2", len(inbound.Blocks))
	}

	doc := inbound.Blocks[0]
	if doc.Type != message.BlockFile {
		t.Errorf("Block[0].Type = %q, want %q", doc.Type, message.BlockFile)
	}
	if doc.URL != "tg://file_id/doc789" {
		t.Errorf("Block[0].URL = %q, want correct document URL", doc.URL)
	}
	if doc.FileName != "report.pdf" {
		t.Errorf("Block[0].FileName = %q, want %q", doc.FileName, "report.pdf")
	}

	caption := inbound.Blocks[1]
	if caption.Text != "Here is the report" {
		t.Errorf("Block[1].Text = %q, want %q", caption.Text, "Here is the report")
	}
}

func TestConvertInbound_LocationMessage(t *testing.T) {
	update := &Update{
		UpdateID: 6,
		Message: &Message{
			MessageID: 47,
			From:      &User{ID: 123, FirstName: "John"},
			Chat:      Chat{ID: 456, Type: "private"},
			Date:      1700000005,
			Location: &Location{
				Latitude:  48.8566,
				Longitude: 2.3522,
			},
		},
	}

	inbound, err := convertInbound(update, "mybot", "telegram")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(inbound.Blocks) != 1 {
		t.Fatalf("len(Blocks) = %d, want 1", len(inbound.Blocks))
	}

	loc := inbound.Blocks[0]
	if loc.Type != message.BlockLocation {
		t.Errorf("Block.Type = %q, want %q", loc.Type, message.BlockLocation)
	}
	if loc.Lat == nil || *loc.Lat != 48.8566 {
		t.Errorf("Block.Lat = %v, want 48.8566", loc.Lat)
	}
	if loc.Lon == nil || *loc.Lon != 2.3522 {
		t.Errorf("Block.Lon = %v, want 2.3522", loc.Lon)
	}
}

func TestConvertInbound_StickerMessage(t *testing.T) {
	update := &Update{
		UpdateID: 7,
		Message: &Message{
			MessageID: 48,
			From:      &User{ID: 123, FirstName: "John"},
			Chat:      Chat{ID: 456, Type: "private"},
			Date:      1700000006,
			Sticker: &Sticker{
				FileID:  "sticker001",
				Emoji:   "\U0001F600",
				SetName: "HappyFaces",
			},
		},
	}

	inbound, err := convertInbound(update, "mybot", "telegram")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(inbound.Blocks) != 1 {
		t.Fatalf("len(Blocks) = %d, want 1", len(inbound.Blocks))
	}

	block := inbound.Blocks[0]
	if block.Type != message.BlockRaw {
		t.Errorf("Block.Type = %q, want %q", block.Type, message.BlockRaw)
	}
	if block.Emoji != "\U0001F600" {
		t.Errorf("Block.Emoji = %q, want grinning face emoji", block.Emoji)
	}

	// Verify raw data contains sticker info.
	var stickerData struct {
		SetName string `json:"set_name"`
		Emoji   string `json:"emoji"`
		FileID  string `json:"file_id"`
	}
	if err := json.Unmarshal(block.Data, &stickerData); err != nil {
		t.Fatalf("unmarshal sticker data: %v", err)
	}
	if stickerData.SetName != "HappyFaces" {
		t.Errorf("sticker set_name = %q, want %q", stickerData.SetName, "HappyFaces")
	}
	if stickerData.FileID != "sticker001" {
		t.Errorf("sticker file_id = %q, want %q", stickerData.FileID, "sticker001")
	}
}

func TestConvertInbound_ChatTypes(t *testing.T) {
	tests := []struct {
		name     string
		tgType   string
		wantType message.ChatType
	}{
		{"private", "private", message.ChatDM},
		{"group", "group", message.ChatGroup},
		{"supergroup", "supergroup", message.ChatGroup},
		{"channel", "channel", message.ChatBroadcast},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			update := &Update{
				UpdateID: 1,
				Message: &Message{
					MessageID: 1,
					From:      &User{ID: 1, FirstName: "Test"},
					Chat:      Chat{ID: 1, Type: tt.tgType},
					Date:      1700000000,
					Text:      "test",
				},
			}

			inbound, err := convertInbound(update, "mybot", "telegram")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if inbound.Chat.Type != tt.wantType {
				t.Errorf("Chat.Type = %q, want %q", inbound.Chat.Type, tt.wantType)
			}
		})
	}
}

func TestConvertInbound_Mentions(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		entities    []MessageEntity
		botUsername string
		wantMention bool
		wantIDs     []string
	}{
		{
			name: "bot mentioned",
			text: "Hey @mybot do something",
			entities: []MessageEntity{
				{Type: "mention", Offset: 4, Length: 6},
			},
			botUsername: "mybot",
			wantMention: true,
			wantIDs:     []string{"mybot"},
		},
		{
			name: "other user mentioned",
			text: "Hey @otheruser check this",
			entities: []MessageEntity{
				{Type: "mention", Offset: 4, Length: 10},
			},
			botUsername: "mybot",
			wantMention: false,
			wantIDs:     []string{"otheruser"},
		},
		{
			name: "text_mention user without username",
			text: "Hey John check this",
			entities: []MessageEntity{
				{Type: "text_mention", Offset: 4, Length: 4, User: &User{ID: 999, FirstName: "John"}},
			},
			botUsername: "mybot",
			wantMention: false,
			wantIDs:     []string{"999"},
		},
		{
			name: "bot mentioned case insensitive",
			text: "Hey @MyBot do something",
			entities: []MessageEntity{
				{Type: "mention", Offset: 4, Length: 6},
			},
			botUsername: "mybot",
			wantMention: true,
			wantIDs:     []string{"MyBot"},
		},
		{
			name:        "no mentions",
			text:        "Just a regular message",
			botUsername: "mybot",
			wantMention: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			update := &Update{
				UpdateID: 1,
				Message: &Message{
					MessageID: 1,
					From:      &User{ID: 1, FirstName: "Test"},
					Chat:      Chat{ID: 1, Type: "group"},
					Date:      1700000000,
					Text:      tt.text,
					Entities:  tt.entities,
				},
			}

			inbound, err := convertInbound(update, tt.botUsername, "telegram")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantMention {
				if inbound.Mentions == nil || !inbound.Mentions.IsMentioned {
					t.Error("IsMentioned = false, want true")
				}
			} else if tt.wantIDs != nil {
				if inbound.Mentions == nil {
					t.Fatal("Mentions = nil, want non-nil")
				}
				if inbound.Mentions.IsMentioned {
					t.Error("IsMentioned = true, want false")
				}
			} else {
				if inbound.Mentions != nil {
					t.Errorf("Mentions = %+v, want nil", inbound.Mentions)
				}
			}

			if tt.wantIDs != nil {
				if inbound.Mentions == nil {
					t.Fatal("Mentions = nil, want non-nil for IDs check")
				}
				if len(inbound.Mentions.IDs) != len(tt.wantIDs) {
					t.Fatalf("len(Mentions.IDs) = %d, want %d", len(inbound.Mentions.IDs), len(tt.wantIDs))
				}
				for i, id := range tt.wantIDs {
					if inbound.Mentions.IDs[i] != id {
						t.Errorf("Mentions.IDs[%d] = %q, want %q", i, inbound.Mentions.IDs[i], id)
					}
				}
			}
		})
	}
}

func TestConvertInbound_Reply(t *testing.T) {
	update := &Update{
		UpdateID: 1,
		Message: &Message{
			MessageID: 50,
			From:      &User{ID: 123, FirstName: "John"},
			Chat:      Chat{ID: 456, Type: "group"},
			Date:      1700000000,
			Text:      "This is a reply",
			ReplyToMessage: &Message{
				MessageID: 49,
			},
		},
	}

	inbound, err := convertInbound(update, "mybot", "telegram")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inbound.ReplyToID != "49" {
		t.Errorf("ReplyToID = %q, want %q", inbound.ReplyToID, "49")
	}
}

func TestConvertInbound_Thread(t *testing.T) {
	update := &Update{
		UpdateID: 1,
		Message: &Message{
			MessageID:       51,
			From:            &User{ID: 123, FirstName: "John"},
			Chat:            Chat{ID: 456, Type: "supergroup"},
			Date:            1700000000,
			Text:            "Thread message",
			MessageThreadID: 100,
		},
	}

	inbound, err := convertInbound(update, "mybot", "telegram")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inbound.ThreadID != "100" {
		t.Errorf("ThreadID = %q, want %q", inbound.ThreadID, "100")
	}
}

func TestConvertInbound_EditedMessage(t *testing.T) {
	update := &Update{
		UpdateID: 1,
		EditedMessage: &Message{
			MessageID: 52,
			From:      &User{ID: 123, FirstName: "John"},
			Chat:      Chat{ID: 456, Type: "private"},
			Date:      1700000000,
			Text:      "Edited text",
		},
	}

	inbound, err := convertInbound(update, "mybot", "telegram")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inbound.ID != "52" {
		t.Errorf("ID = %q, want %q", inbound.ID, "52")
	}
	if inbound.Blocks[0].Text != "Edited text" {
		t.Errorf("Block.Text = %q, want %q", inbound.Blocks[0].Text, "Edited text")
	}
}

func TestConvertInbound_ChannelPost(t *testing.T) {
	update := &Update{
		UpdateID: 1,
		ChannelPost: &Message{
			MessageID: 53,
			Chat:      Chat{ID: -1001234567, Type: "channel", Title: "My Channel"},
			Date:      1700000000,
			Text:      "Channel announcement",
		},
	}

	inbound, err := convertInbound(update, "mybot", "telegram")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inbound.Chat.Type != message.ChatBroadcast {
		t.Errorf("Chat.Type = %q, want %q", inbound.Chat.Type, message.ChatBroadcast)
	}
	if inbound.Chat.Title != "My Channel" {
		t.Errorf("Chat.Title = %q, want %q", inbound.Chat.Title, "My Channel")
	}
	// Channel posts may have no From.
	if inbound.Sender.ID != "" {
		t.Errorf("Sender.ID = %q, want empty for channel post", inbound.Sender.ID)
	}
}

func TestConvertInbound_EmptyUpdate(t *testing.T) {
	update := &Update{UpdateID: 1}

	_, err := convertInbound(update, "mybot", "telegram")
	if err == nil {
		t.Error("expected error for empty update, got nil")
	}
}

func TestConvertInbound_SenderDisplayNameNoLastName(t *testing.T) {
	update := &Update{
		UpdateID: 1,
		Message: &Message{
			MessageID: 1,
			From:      &User{ID: 1, FirstName: "Alice"},
			Chat:      Chat{ID: 1, Type: "private"},
			Date:      1700000000,
			Text:      "hi",
		},
	}

	inbound, err := convertInbound(update, "mybot", "telegram")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inbound.Sender.DisplayName != "Alice" {
		t.Errorf("Sender.DisplayName = %q, want %q", inbound.Sender.DisplayName, "Alice")
	}
}
