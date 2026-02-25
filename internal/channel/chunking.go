package channel

import (
	"strings"

	"github.com/flemzord/sclaw/pkg/message"
)

// ChunkConfig controls how outbound messages are split when they exceed
// a platform's maximum message length.
type ChunkConfig struct {
	// MaxLength is the maximum number of bytes per chunk.
	// A value <= 0 means no splitting.
	MaxLength int

	// PreserveBlocks avoids splitting inside fenced code blocks (``` ... ```).
	// When true, a code block that fits within MaxLength is kept intact even
	// if it would otherwise be split at a line boundary.
	PreserveBlocks bool
}

// SplitMessage splits an outbound message into multiple messages that each
// respect cfg.MaxLength. Non-text blocks are passed through unchanged in
// the first chunk. If the message already fits, a single-element slice is returned.
func SplitMessage(msg message.OutboundMessage, cfg ChunkConfig) []message.OutboundMessage {
	if cfg.MaxLength <= 0 {
		return []message.OutboundMessage{msg}
	}

	// Separate text blocks from non-text blocks.
	var textParts []string
	var nonText []message.ContentBlock
	for _, b := range msg.Blocks {
		if b.Type == message.BlockText {
			textParts = append(textParts, b.Text)
		} else {
			nonText = append(nonText, b)
		}
	}

	fullText := strings.Join(textParts, "\n")
	if len(fullText) <= cfg.MaxLength {
		return []message.OutboundMessage{msg}
	}

	chunks := splitText(fullText, cfg)

	var result []message.OutboundMessage
	for i, chunk := range chunks {
		out := message.OutboundMessage{
			Channel:   msg.Channel,
			Chat:      msg.Chat,
			ThreadID:  msg.ThreadID,
			ReplyToID: msg.ReplyToID,
			Hints:     msg.Hints,
		}

		var blocks []message.ContentBlock
		// Attach non-text blocks to the first chunk only.
		if i == 0 {
			blocks = append(blocks, nonText...)
		}
		blocks = append(blocks, message.NewTextBlock(chunk))
		out.Blocks = blocks

		result = append(result, out)
	}

	return result
}

// splitText breaks text into chunks respecting MaxLength and optionally
// preserving fenced code blocks.
func splitText(text string, cfg ChunkConfig) []string {
	lines := strings.Split(text, "\n")

	var chunks []string
	var current strings.Builder

	inCodeBlock := false

	for _, line := range lines {
		lineWithNewline := line + "\n"

		isFence := strings.HasPrefix(strings.TrimSpace(line), "```")

		// Track fenced code block boundaries.
		// We update the flag after the overflow check so that the closing
		// fence is still considered "inside" the code block.
		wasInCodeBlock := inCodeBlock
		if isFence {
			inCodeBlock = !inCodeBlock
		}

		// If adding this line would exceed the limit...
		if current.Len()+len(lineWithNewline) > cfg.MaxLength {
			// If we're preserving blocks and inside a code block (including
			// the closing fence line), keep accumulating until the block ends
			// — but only if the accumulated text still has a chance to fit
			// (< 2x limit as a safety valve).
			stillInBlock := wasInCodeBlock || (isFence && !inCodeBlock)
			if cfg.PreserveBlocks && stillInBlock && current.Len() < cfg.MaxLength*2 {
				current.WriteString(lineWithNewline)
				continue
			}

			// Flush the current chunk if non-empty.
			if current.Len() > 0 {
				chunks = append(chunks, strings.TrimRight(current.String(), "\n"))
				current.Reset()
			}

			// If a single line exceeds MaxLength, force-split it.
			if len(lineWithNewline) > cfg.MaxLength {
				chunks = append(chunks, forceSplit(line, cfg.MaxLength)...)
				continue
			}
		}

		current.WriteString(lineWithNewline)
	}

	if current.Len() > 0 {
		chunks = append(chunks, strings.TrimRight(current.String(), "\n"))
	}

	return chunks
}

// forceSplit breaks a single long line into chunks of at most maxLen bytes.
func forceSplit(line string, maxLen int) []string {
	var parts []string
	for len(line) > maxLen {
		parts = append(parts, line[:maxLen])
		line = line[maxLen:]
	}
	if len(line) > 0 {
		parts = append(parts, line)
	}
	return parts
}
