package router

import (
	"slices"

	"github.com/flemzord/sclaw/pkg/message"
)

// GroupPolicyMode defines how the router handles group messages.
type GroupPolicyMode string

const (
	// GroupPolicyRequireMention requires the bot to be mentioned in group messages.
	GroupPolicyRequireMention GroupPolicyMode = "require_mention"
	// GroupPolicyAllowAll processes all messages in group chats.
	GroupPolicyAllowAll GroupPolicyMode = "allow_all"
)

// GroupPolicy controls which messages are processed in group chats.
type GroupPolicy struct {
	Mode      GroupPolicyMode
	Allowlist []string
	Denylist  []string
}

// ShouldProcess determines whether an inbound message should be processed.
// DMs are always processed. In groups, the policy mode and lists are checked.
func (p GroupPolicy) ShouldProcess(msg message.InboundMessage) bool {
	// DM → always process.
	if msg.IsDirectMessage() {
		return true
	}

	// Denylist check (sender ID).
	if slices.Contains(p.Denylist, msg.Sender.ID) {
		return false
	}

	switch p.Mode {
	case GroupPolicyAllowAll:
		return true
	case GroupPolicyRequireMention:
		// If sender is in allowlist, always process.
		if slices.Contains(p.Allowlist, msg.Sender.ID) {
			return true
		}
		// Otherwise require mention.
		if msg.Mentions != nil {
			return msg.Mentions.IsMentioned
		}
		return false
	default:
		return false
	}
}
