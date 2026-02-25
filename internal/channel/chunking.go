package channel

import (
	"strings"
	"unicode/utf8"

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
// respect cfg.MaxLength while preserving the original block order.
// If the message already fits, a single-element slice is returned.
func SplitMessage(msg message.OutboundMessage, cfg ChunkConfig) []message.OutboundMessage {
	if cfg.MaxLength <= 0 {
		return []message.OutboundMessage{msg}
	}

	if len(msg.TextContent()) <= cfg.MaxLength {
		return []message.OutboundMessage{msg}
	}

	var result []message.OutboundMessage
	current := newChunkMessage(msg)
	currentTextLen := 0
	hasText := false

	flush := func() {
		if len(current.Blocks) == 0 {
			return
		}
		result = append(result, current)
		current = newChunkMessage(msg)
		currentTextLen = 0
		hasText = false
	}

	for _, block := range msg.Blocks {
		if block.Type != message.BlockText {
			current.Blocks = append(current.Blocks, block)
			continue
		}

		parts := splitText(block.Text, cfg)
		for _, part := range parts {
			added := len(part)
			if hasText {
				// TextContent() joins text blocks with '\n'.
				added++
			}

			if hasText && currentTextLen+added > cfg.MaxLength {
				flush()
			}

			current.Blocks = append(current.Blocks, message.NewTextBlock(part))
			if hasText {
				currentTextLen++
			}
			currentTextLen += len(part)
			hasText = true
		}
	}

	flush()
	if len(result) == 0 {
		return []message.OutboundMessage{msg}
	}
	return result
}

func newChunkMessage(msg message.OutboundMessage) message.OutboundMessage {
	return message.OutboundMessage{
		Channel:   msg.Channel,
		Chat:      msg.Chat,
		ThreadID:  msg.ThreadID,
		ReplyToID: msg.ReplyToID,
		Hints:     msg.Hints,
	}
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
// It walks back from the cut point to avoid splitting mid-rune in UTF-8 text.
func forceSplit(line string, maxLen int) []string {
	var parts []string
	for len(line) > maxLen {
		cut := maxLen
		for cut > 0 && !utf8.RuneStart(line[cut]) {
			cut--
		}
		if cut == 0 {
			cut = maxLen
		}
		parts = append(parts, line[:cut])
		line = line[cut:]
	}
	if len(line) > 0 {
		parts = append(parts, line)
	}
	return parts
}
