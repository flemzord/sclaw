package telegram

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/flemzord/sclaw/pkg/message"
)

// fileIDRef returns a reference URI for a Telegram file_id.
// This is NOT a download URL — consumers must call Client.GetFile + Client.FileURL
// to resolve it into a real download URL. The tg://file_id/ scheme signals this.
func fileIDRef(fileID string) string {
	return "tg://file_id/" + fileID
}

// convertInbound transforms a Telegram Update into a platform-agnostic InboundMessage.
func convertInbound(update *Update, botUsername, channelName string) (message.InboundMessage, error) {
	msg := extractMessage(update)
	if msg == nil {
		return message.InboundMessage{}, fmt.Errorf("telegram: update %d contains no message", update.UpdateID)
	}

	raw, err := json.Marshal(update)
	if err != nil {
		return message.InboundMessage{}, fmt.Errorf("telegram: marshal update: %w", err)
	}

	inbound := message.InboundMessage{
		ID:        strconv.Itoa(msg.MessageID),
		Timestamp: time.Unix(int64(msg.Date), 0),
		Channel:   channelName,
		Sender:    convertSender(msg.From),
		Chat:      convertChat(msg.Chat),
		Raw:       raw,
	}

	if msg.MessageThreadID != 0 {
		inbound.ThreadID = strconv.Itoa(msg.MessageThreadID)
	}
	if msg.ReplyToMessage != nil {
		inbound.ReplyToID = strconv.Itoa(msg.ReplyToMessage.MessageID)
	}

	inbound.Blocks = convertBlocks(msg)
	inbound.Mentions = extractMentions(msg, botUsername)

	return inbound, nil
}

// extractMessage returns the actual message from an Update, checking
// Message, EditedMessage, and ChannelPost in order.
func extractMessage(update *Update) *Message {
	if update.Message != nil {
		return update.Message
	}
	if update.EditedMessage != nil {
		return update.EditedMessage
	}
	return update.ChannelPost
}

// convertSender maps a Telegram User to a platform-agnostic Sender.
func convertSender(user *User) message.Sender {
	if user == nil {
		return message.Sender{}
	}
	displayName := user.FirstName
	if user.LastName != "" {
		displayName += " " + user.LastName
	}
	return message.Sender{
		ID:          strconv.FormatInt(user.ID, 10),
		Username:    user.Username,
		DisplayName: displayName,
	}
}

// convertChat maps a Telegram Chat to a platform-agnostic Chat.
func convertChat(chat Chat) message.Chat {
	return message.Chat{
		ID:    strconv.FormatInt(chat.ID, 10),
		Type:  mapChatType(chat.Type),
		Title: chat.Title,
	}
}

// mapChatType converts Telegram chat type strings to message.ChatType.
func mapChatType(tgType string) message.ChatType {
	switch tgType {
	case "private":
		return message.ChatDM
	case "group", "supergroup":
		return message.ChatGroup
	case "channel":
		return message.ChatBroadcast
	default:
		return message.ChatGroup
	}
}

// convertBlocks builds content blocks from a Telegram message.
// Media URLs use a tg://file_id/ reference that must be resolved lazily via GetFile.
func convertBlocks(msg *Message) []message.ContentBlock {
	var blocks []message.ContentBlock

	switch {
	case len(msg.Photo) > 0:
		largest := msg.Photo[len(msg.Photo)-1]
		blocks = append(blocks, message.NewImageBlock(fileIDRef(largest.FileID), ""))
	case msg.Audio != nil:
		blocks = append(blocks, message.NewAudioBlock(fileIDRef(msg.Audio.FileID), msg.Audio.MIMEType, false))
	case msg.Voice != nil:
		blocks = append(blocks, message.NewAudioBlock(fileIDRef(msg.Voice.FileID), msg.Voice.MIMEType, true))
	case msg.Document != nil:
		blocks = append(blocks, message.NewFileBlock(fileIDRef(msg.Document.FileID), msg.Document.MIMEType, msg.Document.FileName))
	case msg.Location != nil:
		blocks = append(blocks, message.NewLocationBlock(msg.Location.Latitude, msg.Location.Longitude))
	case msg.Sticker != nil:
		blocks = append(blocks, convertSticker(msg.Sticker))
	}

	// Append caption as a text block after media blocks.
	if msg.Caption != "" {
		blocks = append(blocks, message.NewTextBlock(msg.Caption))
	}

	// If no media was found, use the text field.
	if len(blocks) == 0 && msg.Text != "" {
		blocks = append(blocks, message.NewTextBlock(msg.Text))
	}

	return blocks
}

// convertSticker creates a BlockRaw from a Telegram Sticker.
func convertSticker(sticker *Sticker) message.ContentBlock {
	data, _ := json.Marshal(struct {
		SetName string `json:"set_name,omitempty"`
		Emoji   string `json:"emoji,omitempty"`
		FileID  string `json:"file_id"`
	}{
		SetName: sticker.SetName,
		Emoji:   sticker.Emoji,
		FileID:  sticker.FileID,
	})

	block := message.NewRawBlock(data)
	block.Emoji = sticker.Emoji
	return block
}

// extractMentions scans message entities for mentions and detects bot mentions.
func extractMentions(msg *Message, botUsername string) *message.Mentions {
	entities := msg.Entities
	if entities == nil {
		entities = msg.CaptionEntities
	}
	if len(entities) == 0 {
		return nil
	}

	text := msg.Text
	if text == "" {
		text = msg.Caption
	}

	var mentions message.Mentions
	normalizedBot := strings.ToLower(botUsername)

	for _, ent := range entities {
		switch ent.Type {
		case "mention":
			// @username mentions — extract username from text.
			username := extractEntityText(text, ent.Offset, ent.Length)
			username = strings.TrimPrefix(username, "@")
			if username != "" {
				mentions.IDs = append(mentions.IDs, username)
				if strings.EqualFold(username, normalizedBot) {
					mentions.IsMentioned = true
				}
			}
		case "text_mention":
			// Mentions for users without usernames — use the User.ID.
			if ent.User != nil {
				mentions.IDs = append(mentions.IDs, strconv.FormatInt(ent.User.ID, 10))
			}
		}
	}

	if mentions.IsEmpty() {
		return nil
	}
	return &mentions
}

// extractEntityText safely extracts a substring from text using UTF-16 offsets,
// which is what Telegram uses for entity offsets and lengths.
// Telegram encodes offsets as UTF-16 code units, so we must convert
// to UTF-16, slice, and convert back to handle non-BMP characters (emojis).
func extractEntityText(text string, offset, length int) string {
	encoded := utf16.Encode([]rune(text))
	if offset >= len(encoded) {
		return ""
	}
	end := offset + length
	if end > len(encoded) {
		end = len(encoded)
	}
	return string(utf16.Decode(encoded[offset:end]))
}
