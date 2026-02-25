package router

import (
	"testing"

	"github.com/flemzord/sclaw/pkg/message"
)

func TestGroupPolicy_RequireMention(t *testing.T) {
	t.Parallel()

	policy := GroupPolicy{
		Mode: GroupPolicyRequireMention,
	}

	// Group message without mention → false.
	msgNoMention := message.InboundMessage{
		ID:      "msg-1",
		Channel: "slack",
		Chat: message.Chat{
			ID:   "C123",
			Type: message.ChatGroup,
		},
		ThreadID: "T456",
		Sender: message.Sender{
			ID:       "U001",
			Username: "testuser",
		},
		Blocks: []message.ContentBlock{
			message.NewTextBlock("hello"),
		},
		Mentions: &message.Mentions{IsMentioned: false},
	}
	if policy.ShouldProcess(msgNoMention) {
		t.Error("expected ShouldProcess=false for group message without mention")
	}

	// Group message with mention → true.
	msgWithMention := message.InboundMessage{
		ID:      "msg-2",
		Channel: "slack",
		Chat: message.Chat{
			ID:   "C123",
			Type: message.ChatGroup,
		},
		ThreadID: "T456",
		Sender: message.Sender{
			ID:       "U001",
			Username: "testuser",
		},
		Blocks: []message.ContentBlock{
			message.NewTextBlock("hello @bot"),
		},
		Mentions: &message.Mentions{IsMentioned: true},
	}
	if !policy.ShouldProcess(msgWithMention) {
		t.Error("expected ShouldProcess=true for group message with mention")
	}

	// Group message with nil mentions → false.
	msgNilMentions := message.InboundMessage{
		ID:      "msg-3",
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
			message.NewTextBlock("hello"),
		},
	}
	if policy.ShouldProcess(msgNilMentions) {
		t.Error("expected ShouldProcess=false for group message with nil mentions")
	}
}

func TestGroupPolicy_RequireMention_Allowlist(t *testing.T) {
	t.Parallel()

	policy := GroupPolicy{
		Mode:      GroupPolicyRequireMention,
		Allowlist: []string{"U001"},
	}

	// Allowlisted sender in group without mention → true.
	msg := message.InboundMessage{
		ID:      "msg-1",
		Channel: "slack",
		Chat: message.Chat{
			ID:   "C123",
			Type: message.ChatGroup,
		},
		Sender: message.Sender{
			ID:       "U001",
			Username: "trusted",
		},
		Blocks: []message.ContentBlock{
			message.NewTextBlock("hello"),
		},
		Mentions: &message.Mentions{IsMentioned: false},
	}
	if !policy.ShouldProcess(msg) {
		t.Error("expected ShouldProcess=true for allowlisted sender without mention")
	}
}

func TestGroupPolicy_Denylist(t *testing.T) {
	t.Parallel()

	policy := GroupPolicy{
		Mode:     GroupPolicyRequireMention,
		Denylist: []string{"U999"},
	}

	// Denylisted sender → false even if mentioned.
	msg := message.InboundMessage{
		ID:      "msg-1",
		Channel: "slack",
		Chat: message.Chat{
			ID:   "C123",
			Type: message.ChatGroup,
		},
		Sender: message.Sender{
			ID:       "U999",
			Username: "blocked",
		},
		Blocks: []message.ContentBlock{
			message.NewTextBlock("hello @bot"),
		},
		Mentions: &message.Mentions{IsMentioned: true},
	}
	if policy.ShouldProcess(msg) {
		t.Error("expected ShouldProcess=false for denylisted sender even with mention")
	}
}

func TestGroupPolicy_DM_AlwaysAllowed(t *testing.T) {
	t.Parallel()

	// Even with a restrictive policy and denylisted sender, DMs are always processed.
	policy := GroupPolicy{
		Mode:     GroupPolicyRequireMention,
		Denylist: []string{"U001"},
	}

	msg := message.InboundMessage{
		ID:      "msg-1",
		Channel: "slack",
		Chat: message.Chat{
			ID:   "D123",
			Type: message.ChatDM,
		},
		Sender: message.Sender{
			ID:       "U001",
			Username: "testuser",
		},
		Blocks: []message.ContentBlock{
			message.NewTextBlock("hello"),
		},
	}
	if !policy.ShouldProcess(msg) {
		t.Error("expected ShouldProcess=true for DM regardless of policy")
	}
}

func TestGroupPolicy_AllowAll(t *testing.T) {
	t.Parallel()

	policy := GroupPolicy{
		Mode: GroupPolicyAllowAll,
	}

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
			message.NewTextBlock("hello"),
		},
	}
	if !policy.ShouldProcess(msg) {
		t.Error("expected ShouldProcess=true for AllowAll mode in group")
	}
}

func TestGroupPolicy_UnknownMode(t *testing.T) {
	t.Parallel()

	policy := GroupPolicy{
		Mode: "unknown_mode",
	}

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
			message.NewTextBlock("hello"),
		},
	}
	if policy.ShouldProcess(msg) {
		t.Error("expected ShouldProcess=false for unknown policy mode in group")
	}
}
