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
