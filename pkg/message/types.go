// Package message defines the platform-agnostic data contract between channels and the agent.
// It supports text, media, threads, reactions, mentions, and multimodal messages.
package message

// ChatType indicates the kind of conversation.
type ChatType string

const (
	// ChatDM is a direct (one-to-one) conversation.
	ChatDM ChatType = "dm"
	// ChatGroup is a multi-participant group conversation.
	ChatGroup ChatType = "group"
	// ChatBroadcast is a one-to-many broadcast channel.
	ChatBroadcast ChatType = "broadcast"
)

// BlockType discriminates the variant stored in a ContentBlock.
type BlockType string

// Supported block types.
const (
	BlockText     BlockType = "text"
	BlockImage    BlockType = "image"
	BlockAudio    BlockType = "audio"
	BlockFile     BlockType = "file"
	BlockLocation BlockType = "location"
	BlockReaction BlockType = "reaction"
	BlockRaw      BlockType = "raw"
)

// Sender identifies the author of an inbound message.
type Sender struct {
	ID          string `json:"id"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

// Chat identifies the conversation a message belongs to.
type Chat struct {
	ID    string   `json:"id"`
	Type  ChatType `json:"type"`
	Title string   `json:"title,omitempty"`
}

// IsGroup reports whether the chat is a group conversation.
func (c Chat) IsGroup() bool {
	return c.Type == ChatGroup
}

// IsDirectMessage reports whether the chat is a direct message.
func (c Chat) IsDirectMessage() bool {
	return c.Type == ChatDM
}

// Mentions holds mention metadata extracted from an inbound message.
type Mentions struct {
	// IDs lists the user identifiers that were mentioned.
	IDs []string `json:"ids,omitempty"`
	// IsMentioned is true when the bot itself was mentioned.
	IsMentioned bool `json:"is_mentioned,omitempty"`
}

// IsEmpty reports whether the Mentions carries no data.
func (m *Mentions) IsEmpty() bool {
	return m == nil || (len(m.IDs) == 0 && !m.IsMentioned)
}
