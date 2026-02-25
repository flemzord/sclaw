package channel

import (
	"strings"
	"testing"

	"github.com/flemzord/sclaw/pkg/message"
)

func textMsg(text string) message.OutboundMessage {
	return message.OutboundMessage{
		Channel: "test",
		Chat:    message.Chat{ID: "chat-1"},
		Blocks:  []message.ContentBlock{message.NewTextBlock(text)},
	}
}

func TestSplitMessage_NoChunkingWhenDisabled(t *testing.T) {
	t.Parallel()
	msg := textMsg("hello world")
	result := SplitMessage(msg, ChunkConfig{MaxLength: 0})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
}

func TestSplitMessage_ShortMessageUnchanged(t *testing.T) {
	t.Parallel()
	msg := textMsg("hello world")
	result := SplitMessage(msg, ChunkConfig{MaxLength: 100})
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Blocks[0].Text != "hello world" {
		t.Errorf("text mismatch: %q", result[0].Blocks[0].Text)
	}
}

func TestSplitMessage_SplitsLongText(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("a", 100) + "\n" + strings.Repeat("b", 100)
	msg := textMsg(text)
	result := SplitMessage(msg, ChunkConfig{MaxLength: 110})
	if len(result) < 2 {
		t.Fatalf("expected >= 2 chunks, got %d", len(result))
	}
	for i, r := range result {
		content := r.TextContent()
		if len(content) > 110 {
			t.Errorf("chunk %d exceeds max length: %d > 110", i, len(content))
		}
	}
}

func TestSplitMessage_PreservesCodeBlocks(t *testing.T) {
	t.Parallel()
	code := "```\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```"
	text := "Before\n" + code + "\nAfter"
	msg := textMsg(text)
	// MaxLength large enough to hold the code block but not everything.
	result := SplitMessage(msg, ChunkConfig{MaxLength: len(code) + 10, PreserveBlocks: true})

	// The code block should appear intact in one chunk.
	found := false
	for _, r := range result {
		if strings.Contains(r.TextContent(), code) {
			found = true
			break
		}
	}
	if !found {
		t.Error("code block was split across chunks")
	}
}

func TestSplitMessage_PreserveBlocksStillRespectsMaxLength(t *testing.T) {
	t.Parallel()

	code := "```\n" + strings.Repeat("x", 120) + "\n```"
	msg := textMsg("Before\n" + code + "\nAfter")
	maxLen := 60

	result := SplitMessage(msg, ChunkConfig{
		MaxLength:      maxLen,
		PreserveBlocks: true,
	})

	if len(result) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(result))
	}
	for i, r := range result {
		content := r.TextContent()
		if len(content) > maxLen {
			t.Fatalf("chunk %d exceeds max length: %d > %d", i, len(content), maxLen)
		}
	}
}

func TestSplitMessage_NonTextBlocksInFirstChunk(t *testing.T) {
	t.Parallel()
	msg := message.OutboundMessage{
		Channel: "test",
		Chat:    message.Chat{ID: "chat-1"},
		Blocks: []message.ContentBlock{
			message.NewImageBlock("https://example.com/img.png", "image/png"),
			message.NewTextBlock(strings.Repeat("a", 100) + "\n" + strings.Repeat("b", 100)),
		},
	}
	result := SplitMessage(msg, ChunkConfig{MaxLength: 110})
	if len(result) < 2 {
		t.Fatalf("expected >= 2 chunks, got %d", len(result))
	}
	// First chunk should have the image block.
	hasImage := false
	for _, b := range result[0].Blocks {
		if b.Type == message.BlockImage {
			hasImage = true
		}
	}
	if !hasImage {
		t.Error("first chunk should contain the image block")
	}
	// Second chunk should NOT have the image block.
	for _, b := range result[1].Blocks {
		if b.Type == message.BlockImage {
			t.Error("subsequent chunks should not contain non-text blocks")
		}
	}
}

func TestSplitMessage_PreservesMetadata(t *testing.T) {
	t.Parallel()
	msg := message.OutboundMessage{
		Channel:   "test-ch",
		Chat:      message.Chat{ID: "chat-1"},
		ThreadID:  "thread-42",
		ReplyToID: "msg-99",
		Blocks:    []message.ContentBlock{message.NewTextBlock(strings.Repeat("x", 200))},
	}
	result := SplitMessage(msg, ChunkConfig{MaxLength: 100})
	for i, r := range result {
		if r.Channel != "test-ch" {
			t.Errorf("chunk %d: Channel = %q, want %q", i, r.Channel, "test-ch")
		}
		if r.ThreadID != "thread-42" {
			t.Errorf("chunk %d: ThreadID = %q, want %q", i, r.ThreadID, "thread-42")
		}
		if r.ReplyToID != "msg-99" {
			t.Errorf("chunk %d: ReplyToID = %q, want %q", i, r.ReplyToID, "msg-99")
		}
	}
}

func TestSplitText_ForceSplitLongLine(t *testing.T) {
	t.Parallel()
	// A single line longer than MaxLength should be force-split.
	long := strings.Repeat("x", 250)
	msg := textMsg(long)
	result := SplitMessage(msg, ChunkConfig{MaxLength: 100})
	if len(result) < 3 {
		t.Fatalf("expected >= 3 chunks for 250 char line with max 100, got %d", len(result))
	}
	// Reconstruct and verify nothing was lost.
	var rebuilt string
	for _, r := range result {
		rebuilt += r.TextContent()
	}
	if rebuilt != long {
		t.Errorf("reconstructed text length = %d, want %d", len(rebuilt), len(long))
	}
}

func TestSplitMessage_EmptyText(t *testing.T) {
	t.Parallel()
	msg := textMsg("")
	result := SplitMessage(msg, ChunkConfig{MaxLength: 100})
	if len(result) != 1 {
		t.Fatalf("expected 1 message for empty text, got %d", len(result))
	}
}
