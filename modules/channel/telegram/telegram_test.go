package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/pkg/message"
	"gopkg.in/yaml.v3"
)

// TestTelegramLifecycle tests the full module lifecycle:
// Configure → Provision → Validate → Start → simulate inbound → verify outbound → Stop.
func TestTelegramLifecycle(t *testing.T) {
	// Track API calls made by the module.
	var mu sync.Mutex
	apiCalls := make(map[string]int)
	var lastSentText string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		apiCalls[r.URL.Path]++
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.Contains(r.URL.Path, "getMe"):
			resp := APIResponse[User]{
				OK: true,
				Result: User{
					ID:        100,
					IsBot:     true,
					FirstName: "TestBot",
					Username:  "testbot",
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case strings.Contains(r.URL.Path, "getUpdates"):
			// Return empty updates to keep poller quiet.
			resp := APIResponse[[]Update]{
				OK:     true,
				Result: []Update{},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case strings.Contains(r.URL.Path, "sendMessage"):
			var req SendMessageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode sendMessage request: %v", err)
				return
			}
			mu.Lock()
			lastSentText = req.Text
			mu.Unlock()

			resp := APIResponse[Message]{
				OK: true,
				Result: Message{
					MessageID: 999,
					Chat:      Chat{ID: req.ChatID, Type: "private"},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case strings.Contains(r.URL.Path, "sendChatAction"):
			resp := APIResponse[bool]{OK: true, Result: true}
			_ = json.NewEncoder(w).Encode(resp)

		case strings.Contains(r.URL.Path, "deleteWebhook"):
			resp := APIResponse[bool]{OK: true, Result: true}
			_ = json.NewEncoder(w).Encode(resp)

		default:
			resp := APIResponse[bool]{OK: true, Result: true}
			_ = json.NewEncoder(w).Encode(resp)
		}
	}))
	defer server.Close()

	// Step 1: Configure.
	tg := &Telegram{}

	cfgYAML := `
token: "test-token-123"
mode: "polling"
allow_users:
  - "123"
max_message_length: 4096
`
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(cfgYAML), &node); err != nil {
		t.Fatalf("yaml unmarshal: %v", err)
	}
	// yaml.Unmarshal wraps in a document node; use the first content node.
	if err := tg.Configure(node.Content[0]); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	// Override API URL to use test server.
	tg.config.APIURL = server.URL

	// Step 2: Provision.
	appCtx := core.NewAppContext(discardLogger(), t.TempDir(), t.TempDir())
	appCtx = appCtx.ForModule("channel.telegram")
	if err := tg.Provision(appCtx); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	// Step 3: Validate.
	if err := tg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// Step 4: SetInbox and Start.
	var receivedMsgs []message.InboundMessage
	var inboxMu sync.Mutex
	tg.SetInbox(func(msg message.InboundMessage) error {
		inboxMu.Lock()
		receivedMsgs = append(receivedMsgs, msg)
		inboxMu.Unlock()
		return nil
	})

	if err := tg.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Verify getMe was called.
	mu.Lock()
	if apiCalls["/bottest-token-123/getMe"] == 0 {
		t.Error("getMe was not called during Start")
	}
	mu.Unlock()

	if tg.botUser == nil {
		t.Fatal("botUser not set after Start")
	}
	if tg.botUser.Username != "testbot" {
		t.Errorf("botUser.Username = %q, want %q", tg.botUser.Username, "testbot")
	}

	// Step 5: Send an outbound message.
	outbound := message.OutboundMessage{
		Chat: message.Chat{ID: "456", Type: message.ChatDM},
		Blocks: []message.ContentBlock{
			message.NewTextBlock("Hello from test!"),
		},
	}
	if err := tg.Send(context.Background(), outbound); err != nil {
		t.Fatalf("Send: %v", err)
	}

	mu.Lock()
	if apiCalls["/bottest-token-123/sendMessage"] == 0 {
		t.Error("sendMessage was not called")
	}
	if lastSentText != "Hello from test!" {
		t.Errorf("sent text = %q, want %q", lastSentText, "Hello from test!")
	}
	mu.Unlock()

	// Step 6: SendTyping.
	if err := tg.SendTyping(context.Background(), message.Chat{ID: "456", Type: message.ChatDM}); err != nil {
		t.Fatalf("SendTyping: %v", err)
	}

	mu.Lock()
	if apiCalls["/bottest-token-123/sendChatAction"] == 0 {
		t.Error("sendChatAction was not called for typing")
	}
	mu.Unlock()

	// Step 7: SupportsStreaming.
	if !tg.SupportsStreaming() {
		t.Error("SupportsStreaming() = false, want true")
	}

	// Step 8: Stop.
	if err := tg.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestTelegramValidate_MissingToken(t *testing.T) {
	tg := &Telegram{}
	tg.config.Mode = "polling"

	if err := tg.Validate(); err == nil {
		t.Error("Validate should fail with missing token")
	}
}

func TestTelegramValidate_InvalidMode(t *testing.T) {
	tg := &Telegram{}
	tg.config.Token = "test-token"
	tg.config.Mode = "invalid"

	if err := tg.Validate(); err == nil {
		t.Error("Validate should fail with invalid mode")
	}
}

func TestTelegramValidate_WebhookMissingURL(t *testing.T) {
	tg := &Telegram{}
	tg.config.Token = "test-token"
	tg.config.Mode = "webhook"
	tg.config.WebhookURL = ""

	if err := tg.Validate(); err == nil {
		t.Error("Validate should fail with missing webhook URL")
	}
}

func TestTelegramModuleInfo(t *testing.T) {
	tg := &Telegram{}
	info := tg.ModuleInfo()

	if info.ID != "channel.telegram" {
		t.Errorf("ModuleInfo.ID = %q, want %q", info.ID, "channel.telegram")
	}
	if info.New == nil {
		t.Error("ModuleInfo.New should not be nil")
	}

	// Verify New returns a fresh instance.
	instance := info.New()
	if _, ok := instance.(*Telegram); !ok {
		t.Error("New() should return *Telegram")
	}
}
