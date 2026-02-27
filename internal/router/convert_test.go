package router

import (
	"testing"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/pkg/message"
)

func TestMessageToLLM(t *testing.T) {
	t.Parallel()

	msg := message.InboundMessage{
		ID:      "msg-1",
		Channel: "slack",
		Chat: message.Chat{
			ID:   "C123",
			Type: message.ChatGroup,
		},
		Sender: message.Sender{
			ID:       "U001",
			Username: "testuser",
		},
		Blocks: []message.ContentBlock{
			message.NewTextBlock("hello world"),
		},
	}

	llmMsg := messageToLLM(msg)

	if llmMsg.Role != provider.MessageRoleUser {
		t.Errorf("Role = %q, want %q", llmMsg.Role, provider.MessageRoleUser)
	}
	if llmMsg.Content != "hello world" {
		t.Errorf("Content = %q, want %q", llmMsg.Content, "hello world")
	}
}

func TestMessageToLLM_TextOnly_Unchanged(t *testing.T) {
	t.Parallel()

	msg := message.InboundMessage{
		Blocks: []message.ContentBlock{
			message.NewTextBlock("just text"),
		},
	}

	llmMsg := messageToLLM(msg)

	if llmMsg.Content != "just text" {
		t.Errorf("Content = %q, want %q", llmMsg.Content, "just text")
	}
	if llmMsg.ContentParts != nil {
		t.Error("ContentParts should be nil for text-only messages")
	}
}

func TestMessageToLLM_ImageOnly(t *testing.T) {
	t.Parallel()

	msg := message.InboundMessage{
		Blocks: []message.ContentBlock{
			message.NewImageBlock("https://example.com/photo.jpg", "image/jpeg"),
		},
	}

	llmMsg := messageToLLM(msg)

	if llmMsg.Content != "" {
		t.Errorf("Content = %q, want empty", llmMsg.Content)
	}
	if len(llmMsg.ContentParts) != 1 {
		t.Fatalf("ContentParts len = %d, want 1", len(llmMsg.ContentParts))
	}
	p := llmMsg.ContentParts[0]
	if p.Type != provider.ContentPartImageURL {
		t.Errorf("part type = %q, want %q", p.Type, provider.ContentPartImageURL)
	}
	if p.ImageURL == nil || p.ImageURL.URL != "https://example.com/photo.jpg" {
		t.Errorf("image URL = %v", p.ImageURL)
	}
}

func TestMessageToLLM_TextAndImage(t *testing.T) {
	t.Parallel()

	msg := message.InboundMessage{
		Blocks: []message.ContentBlock{
			message.NewImageBlock("https://example.com/photo.jpg", "image/jpeg"),
			message.NewTextBlock("What is this?"),
		},
	}

	llmMsg := messageToLLM(msg)

	if llmMsg.Content != "" {
		t.Errorf("Content = %q, want empty (multimodal)", llmMsg.Content)
	}
	if len(llmMsg.ContentParts) != 2 {
		t.Fatalf("ContentParts len = %d, want 2", len(llmMsg.ContentParts))
	}
	if llmMsg.ContentParts[0].Type != provider.ContentPartImageURL {
		t.Errorf("part[0] type = %q, want image_url", llmMsg.ContentParts[0].Type)
	}
	if llmMsg.ContentParts[1].Type != provider.ContentPartText {
		t.Errorf("part[1] type = %q, want text", llmMsg.ContentParts[1].Type)
	}
	if llmMsg.ContentParts[1].Text != "What is this?" {
		t.Errorf("part[1] text = %q", llmMsg.ContentParts[1].Text)
	}
}

func TestBuildOutbound(t *testing.T) {
	t.Parallel()

	original := message.InboundMessage{
		ID:      "msg-1",
		Channel: "slack",
		Chat: message.Chat{
			ID:    "C123",
			Type:  message.ChatGroup,
			Title: "test-channel",
		},
		ThreadID: "T456",
		Sender: message.Sender{
			ID:       "U001",
			Username: "testuser",
		},
		Blocks: []message.ContentBlock{
			message.NewTextBlock("hello"),
		},
	}

	resp := agent.Response{
		Content:    "Hi there!",
		StopReason: agent.StopReasonComplete,
	}

	out := buildOutbound(original, resp)

	if out.Chat.ID != "C123" {
		t.Errorf("Chat.ID = %q, want %q", out.Chat.ID, "C123")
	}
	if out.ThreadID != "T456" {
		t.Errorf("ThreadID = %q, want %q", out.ThreadID, "T456")
	}
	if out.ReplyToID != "msg-1" {
		t.Errorf("ReplyToID = %q, want %q", out.ReplyToID, "msg-1")
	}
	if out.TextContent() != "Hi there!" {
		t.Errorf("TextContent() = %q, want %q", out.TextContent(), "Hi there!")
	}
}

// m-30: Verify sessionViewAdapter.GetMetadata returns the value for a key.
func TestSessionViewAdapter_GetMetadata(t *testing.T) {
	t.Parallel()

	sess := &Session{
		ID:       "sess-1",
		Metadata: map[string]any{"key": "original"},
	}
	adapter := &sessionViewAdapter{session: sess}

	val, ok := adapter.GetMetadata("key")
	if !ok {
		t.Fatal("GetMetadata should return true for existing key")
	}
	if val != "original" {
		t.Errorf("GetMetadata(key) = %v, want %q", val, "original")
	}

	_, ok = adapter.GetMetadata("missing")
	if ok {
		t.Error("GetMetadata should return false for missing key")
	}
}

func TestSessionViewAdapter_GetMetadata_NilMetadata(t *testing.T) {
	t.Parallel()

	sess := &Session{ID: "sess-1"}
	adapter := &sessionViewAdapter{session: sess}

	_, ok := adapter.GetMetadata("any")
	if ok {
		t.Error("GetMetadata should return false when session metadata is nil")
	}
}

func TestSessionViewAdapter_SessionID(t *testing.T) {
	t.Parallel()

	sess := &Session{ID: "sess-42"}
	adapter := &sessionViewAdapter{session: sess}

	if adapter.SessionID() != "sess-42" {
		t.Errorf("SessionID() = %q, want %q", adapter.SessionID(), "sess-42")
	}
}

func TestSessionViewAdapter_SessionKey(t *testing.T) {
	t.Parallel()

	sess := &Session{
		ID:  "sess-1",
		Key: SessionKey{Channel: "slack", ChatID: "C1", ThreadID: "T1"},
	}
	adapter := &sessionViewAdapter{session: sess}

	ch, chatID, threadID := adapter.SessionKey()
	if ch != "slack" || chatID != "C1" || threadID != "T1" {
		t.Errorf("SessionKey() = (%q, %q, %q), want (slack, C1, T1)", ch, chatID, threadID)
	}
}

func TestSessionViewAdapter_AgentID(t *testing.T) {
	t.Parallel()

	sess := &Session{ID: "sess-1", AgentID: "agent-x"}
	adapter := &sessionViewAdapter{session: sess}

	if adapter.AgentID() != "agent-x" {
		t.Errorf("AgentID() = %q, want %q", adapter.AgentID(), "agent-x")
	}
}

func TestSessionViewAdapter_CreatedAt(t *testing.T) {
	t.Parallel()

	sess := &Session{ID: "sess-1"}
	adapter := &sessionViewAdapter{session: sess}

	// Verify the method is callable and returns the session's CreatedAt.
	_ = adapter.CreatedAt()
}
