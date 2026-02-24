package message

import "encoding/json"

// ContentBlock is a flat union representing one piece of content inside a message.
// The Type field discriminates which fields are meaningful.
type ContentBlock struct {
	Type     BlockType       `json:"type"`
	Text     string          `json:"text,omitempty"`
	URL      string          `json:"url,omitempty"`
	MIMEType string          `json:"mime_type,omitempty"`
	FileName string          `json:"file_name,omitempty"`
	Caption  string          `json:"caption,omitempty"`
	IsVoice  bool            `json:"is_voice,omitempty"`
	Lat      *float64        `json:"lat,omitempty"`
	Lon      *float64        `json:"lon,omitempty"`
	Emoji    string          `json:"emoji,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
}

// MarshalJSON implements json.Marshaler.
// It enforces union semantics:
// - location blocks always include lat/lon (defaulting to 0 when unset)
// - non-location blocks omit lat/lon
func (b ContentBlock) MarshalJSON() ([]byte, error) {
	type alias ContentBlock
	normalized := b

	if normalized.Type == BlockLocation {
		if normalized.Lat == nil {
			zero := 0.0
			normalized.Lat = &zero
		}
		if normalized.Lon == nil {
			zero := 0.0
			normalized.Lon = &zero
		}
	} else {
		normalized.Lat = nil
		normalized.Lon = nil
	}

	return json.Marshal(alias(normalized))
}

// NewTextBlock creates a text content block.
func NewTextBlock(text string) ContentBlock {
	return ContentBlock{Type: BlockText, Text: text}
}

// NewImageBlock creates an image content block.
func NewImageBlock(url, mimeType string) ContentBlock {
	return ContentBlock{Type: BlockImage, URL: url, MIMEType: mimeType}
}

// NewAudioBlock creates an audio content block. Set isVoice to true for voice messages.
func NewAudioBlock(url, mimeType string, isVoice bool) ContentBlock {
	return ContentBlock{Type: BlockAudio, URL: url, MIMEType: mimeType, IsVoice: isVoice}
}

// NewFileBlock creates a file content block.
func NewFileBlock(url, mimeType, fileName string) ContentBlock {
	return ContentBlock{Type: BlockFile, URL: url, MIMEType: mimeType, FileName: fileName}
}

// NewLocationBlock creates a location content block.
func NewLocationBlock(lat, lon float64) ContentBlock {
	return ContentBlock{Type: BlockLocation, Lat: &lat, Lon: &lon}
}

// NewReactionBlock creates a reaction content block.
func NewReactionBlock(emoji string) ContentBlock {
	return ContentBlock{Type: BlockReaction, Emoji: emoji}
}

// NewRawBlock creates a raw content block carrying opaque JSON data.
func NewRawBlock(data json.RawMessage) ContentBlock {
	cp := make(json.RawMessage, len(data))
	copy(cp, data)
	return ContentBlock{Type: BlockRaw, Data: cp}
}

// textContent concatenates the text of all text blocks, separated by newlines.
func textContent(blocks []ContentBlock) string {
	var result string
	for _, b := range blocks {
		if b.Type == BlockText && b.Text != "" {
			if result != "" {
				result += "\n"
			}
			result += b.Text
		}
	}
	return result
}

// hasMedia reports whether any block is a non-text, non-reaction, non-raw content type.
func hasMedia(blocks []ContentBlock) bool {
	for _, b := range blocks {
		switch b.Type {
		case BlockImage, BlockAudio, BlockFile, BlockLocation:
			return true
		}
	}
	return false
}
