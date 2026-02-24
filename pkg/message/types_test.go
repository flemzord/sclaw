package message

import "testing"

func TestChat_IsGroup(t *testing.T) {
	tests := []struct {
		name string
		chat Chat
		want bool
	}{
		{"group chat", Chat{ID: "1", Type: ChatGroup}, true},
		{"dm chat", Chat{ID: "2", Type: ChatDM}, false},
		{"broadcast chat", Chat{ID: "3", Type: ChatBroadcast}, false},
		{"empty type", Chat{ID: "4"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.chat.IsGroup(); got != tt.want {
				t.Errorf("Chat.IsGroup() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChat_IsDirectMessage(t *testing.T) {
	tests := []struct {
		name string
		chat Chat
		want bool
	}{
		{"dm chat", Chat{ID: "1", Type: ChatDM}, true},
		{"group chat", Chat{ID: "2", Type: ChatGroup}, false},
		{"broadcast chat", Chat{ID: "3", Type: ChatBroadcast}, false},
		{"empty type", Chat{ID: "4"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.chat.IsDirectMessage(); got != tt.want {
				t.Errorf("Chat.IsDirectMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}
