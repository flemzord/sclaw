package channel

import (
	"strings"

	"github.com/flemzord/sclaw/pkg/message"
)

// AllowList controls which users and groups are permitted to interact with
// a channel. An empty or nil AllowList denies everyone — security by default.
type AllowList struct {
	users  map[string]struct{}
	groups map[string]struct{}
}

// NewAllowList creates an AllowList with O(1) lookups. Keys are trimmed and
// lowercased at construction time so that IsAllowed can use direct map lookups.
func NewAllowList(users, groups []string) *AllowList {
	a := &AllowList{
		users:  make(map[string]struct{}, len(users)),
		groups: make(map[string]struct{}, len(groups)),
	}
	for _, u := range users {
		a.users[normalize(u)] = struct{}{}
	}
	for _, g := range groups {
		a.groups[normalize(g)] = struct{}{}
	}
	return a
}

// IsAllowed reports whether the message sender or chat is permitted.
//
// Rules:
//   - If both maps are empty → deny (no one is allowed).
//   - If the sender's ID matches a user entry → allow.
//   - If the chat's ID matches a group entry → allow.
//   - Otherwise → deny.
func (a *AllowList) IsAllowed(msg message.InboundMessage) bool {
	if a == nil || (len(a.users) == 0 && len(a.groups) == 0) {
		return false
	}

	if _, ok := a.users[normalize(msg.Sender.ID)]; ok {
		return true
	}
	if _, ok := a.groups[normalize(msg.Chat.ID)]; ok {
		return true
	}
	return false
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
