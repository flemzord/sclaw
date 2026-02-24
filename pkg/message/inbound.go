package message

import (
	"encoding/json"
	"time"
)

// InboundMessage represents a message received from a channel.
type InboundMessage struct {
	ID        string          `json:"id"`
	Timestamp time.Time       `json:"timestamp"`
	Channel   string          `json:"channel"`
	Sender    Sender          `json:"sender"`
	Chat      Chat            `json:"chat"`
	ThreadID  string          `json:"thread_id,omitempty"`
	ReplyToID string          `json:"reply_to_id,omitempty"`
	Blocks    []ContentBlock  `json:"blocks"`
	Mentions  *Mentions       `json:"mentions,omitempty"`
	Raw       json.RawMessage `json:"raw,omitempty"`
}

// MarshalJSON implements json.Marshaler. It normalizes empty Mentions to nil
// so that the field is omitted from JSON output.
func (m InboundMessage) MarshalJSON() ([]byte, error) {
	if m.Mentions.IsEmpty() {
		m.Mentions = nil
	}
	type alias InboundMessage
	return json.Marshal(alias(m))
}

// TextContent returns the concatenated text of all text blocks.
func (m *InboundMessage) TextContent() string {
	return textContent(m.Blocks)
}

// HasMedia reports whether the message contains media blocks.
func (m *InboundMessage) HasMedia() bool {
	return hasMedia(m.Blocks)
}

// IsGroup reports whether the message was sent in a group chat.
func (m *InboundMessage) IsGroup() bool {
	return m.Chat.IsGroup()
}

// IsDirectMessage reports whether the message is a direct message.
func (m *InboundMessage) IsDirectMessage() bool {
	return m.Chat.IsDirectMessage()
}
