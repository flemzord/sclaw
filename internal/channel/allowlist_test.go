package channel

import (
	"testing"

	"github.com/flemzord/sclaw/pkg/message"
)

func dmMsg(senderID string) message.InboundMessage {
	return message.InboundMessage{
		Sender: message.Sender{ID: senderID},
		Chat:   message.Chat{ID: "chat-1", Type: message.ChatDM},
	}
}

func groupMsg(senderID, groupID string) message.InboundMessage {
	return message.InboundMessage{
		Sender: message.Sender{ID: senderID},
		Chat:   message.Chat{ID: groupID, Type: message.ChatGroup},
	}
}

func TestAllowList_NilDeniesAll(t *testing.T) {
	t.Parallel()
	var a *AllowList
	if a.IsAllowed(dmMsg("alice")) {
		t.Error("nil AllowList should deny everyone")
	}
}

func TestAllowList_EmptyDeniesAll(t *testing.T) {
	t.Parallel()
	a := NewAllowList(nil, nil)
	if a.IsAllowed(dmMsg("alice")) {
		t.Error("empty AllowList should deny everyone")
	}
}

func TestAllowList_DMAllowed(t *testing.T) {
	t.Parallel()
	a := NewAllowList([]string{"alice", "bob"}, nil)

	tests := []struct {
		name    string
		sender  string
		allowed bool
	}{
		{"allowed user", "alice", true},
		{"allowed user 2", "bob", true},
		{"unknown user", "charlie", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := a.IsAllowed(dmMsg(tc.sender))
			if got != tc.allowed {
				t.Errorf("IsAllowed(%q) = %v, want %v", tc.sender, got, tc.allowed)
			}
		})
	}
}

func TestAllowList_GroupAllowed(t *testing.T) {
	t.Parallel()
	a := NewAllowList(nil, []string{"group-1", "group-2"})

	tests := []struct {
		name    string
		groupID string
		allowed bool
	}{
		{"allowed group", "group-1", true},
		{"allowed group 2", "group-2", true},
		{"unknown group", "group-3", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := a.IsAllowed(groupMsg("anyone", tc.groupID))
			if got != tc.allowed {
				t.Errorf("IsAllowed(group %q) = %v, want %v", tc.groupID, got, tc.allowed)
			}
		})
	}
}

func TestAllowList_NormalizesKeys(t *testing.T) {
	t.Parallel()
	// Keys with spaces and mixed case should still match.
	a := NewAllowList([]string{" Alice "}, []string{" Group-1 "})

	if !a.IsAllowed(dmMsg("alice")) {
		t.Error("should allow normalized match for user")
	}
	if !a.IsAllowed(groupMsg("anyone", "group-1")) {
		t.Error("should allow normalized match for group")
	}
}

func TestAllowList_UserInGroupChat(t *testing.T) {
	t.Parallel()
	// The implementation checks users first, then groups for any message.
	// So alice (in user list) sending in a group is allowed via user match.
	a := NewAllowList([]string{"alice"}, nil)
	if !a.IsAllowed(groupMsg("alice", "group-1")) {
		t.Error("alice should be allowed in group via user match")
	}
	// bob (not in user list) in same group should be denied.
	if a.IsAllowed(groupMsg("bob", "group-1")) {
		t.Error("bob should not be allowed without user or group match")
	}
}

func TestAllowList_BothUsersAndGroups(t *testing.T) {
	t.Parallel()
	a := NewAllowList([]string{"alice"}, []string{"group-1"})

	// DM from alice → allowed via user list.
	if !a.IsAllowed(dmMsg("alice")) {
		t.Error("DM from allowed user should pass")
	}
	// DM from bob → denied.
	if a.IsAllowed(dmMsg("bob")) {
		t.Error("DM from unknown user should be denied")
	}
	// Group message in allowed group → allowed.
	if !a.IsAllowed(groupMsg("bob", "group-1")) {
		t.Error("message in allowed group should pass")
	}
	// Group message in unknown group from unknown user → denied.
	if a.IsAllowed(groupMsg("bob", "group-2")) {
		t.Error("message in unknown group from unknown user should be denied")
	}
}
