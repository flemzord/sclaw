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
	currentLen := 0

	flush := func() {
		if currentLen == 0 {
			return
		}
		chunks = append(chunks, current.String())
		current.Reset()
		currentLen = 0
	}

	for i := 0; i < len(lines); {
		line := lines[i]

		// Best-effort block preservation: if a fenced block fully fits the max
		// length, move it as a single unit instead of splitting it line by line.
		if cfg.PreserveBlocks && isFenceLine(line) {
			end, found := findFenceEnd(lines, i)
			if found {
				block := strings.Join(lines[i:end+1], "\n")
				if len(block) <= cfg.MaxLength {
					appendPiece(&current, &currentLen, &chunks, block, cfg.MaxLength)
					i = end + 1
					continue
				}
			}
		}

		appendLine(&current, &currentLen, &chunks, line, cfg.MaxLength)
		i++
	}

	flush()

	return chunks
}

func appendPiece(current *strings.Builder, currentLen *int, chunks *[]string, piece string, maxLen int) {
	if piece == "" {
		appendLine(current, currentLen, chunks, "", maxLen)
		return
	}

	added := len(piece)
	if *currentLen > 0 {
		added++
	}
	if *currentLen+added > maxLen {
		if *currentLen > 0 {
			*chunks = append(*chunks, current.String())
			current.Reset()
			*currentLen = 0
		}
	}

	if len(piece) <= maxLen {
		if *currentLen > 0 {
			current.WriteByte('\n')
			*currentLen = *currentLen + 1
		}
		current.WriteString(piece)
		*currentLen += len(piece)
		return
	}

	*chunks = append(*chunks, forceSplit(piece, maxLen)...)
}

func appendLine(current *strings.Builder, currentLen *int, chunks *[]string, line string, maxLen int) {
	if len(line) > maxLen {
		if *currentLen > 0 {
			*chunks = append(*chunks, current.String())
			current.Reset()
			*currentLen = 0
		}
		*chunks = append(*chunks, forceSplit(line, maxLen)...)
		return
	}

	added := len(line)
	if *currentLen > 0 {
		added++
	}
	if *currentLen+added > maxLen {
		*chunks = append(*chunks, current.String())
		current.Reset()
		*currentLen = 0
	}
	if *currentLen > 0 {
		current.WriteByte('\n')
		*currentLen = *currentLen + 1
	}
	current.WriteString(line)
	*currentLen += len(line)
}

func isFenceLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "```")
}

func findFenceEnd(lines []string, start int) (int, bool) {
	for i := start + 1; i < len(lines); i++ {
		if isFenceLine(lines[i]) {
			return i, true
		}
	}
	return -1, false
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
