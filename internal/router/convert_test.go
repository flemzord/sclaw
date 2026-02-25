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
