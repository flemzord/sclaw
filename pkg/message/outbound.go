package message

// OutboundMessage represents a message to be sent through a channel.
type OutboundMessage struct {
	Chat      Chat           `json:"chat"`
	ThreadID  string         `json:"thread_id,omitempty"`
	ReplyToID string         `json:"reply_to_id,omitempty"`
	Blocks    []ContentBlock `json:"blocks"`
	Hints     *OutboundHints `json:"hints,omitempty"`
}

// OutboundHints carries optional delivery hints for channels.
// Zero value means no hints are set.
type OutboundHints struct {
	DisablePreview      bool   `json:"disable_preview,omitempty"`
	DisableNotification bool   `json:"disable_notification,omitempty"`
	ParseMode           string `json:"parse_mode,omitempty"`
}

// NewTextMessage creates an outbound message with a single text block.
func NewTextMessage(chat Chat, text string) OutboundMessage {
	return OutboundMessage{
		Chat:   chat,
		Blocks: []ContentBlock{NewTextBlock(text)},
	}
}

// TextContent returns the concatenated text of all text blocks.
func (m *OutboundMessage) TextContent() string {
	return textContent(m.Blocks)
}

// HasMedia reports whether the message contains media blocks.
func (m *OutboundMessage) HasMedia() bool {
	return hasMedia(m.Blocks)
}
