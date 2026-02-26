package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/channel"
	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/pkg/message"
	"gopkg.in/yaml.v3"
)

// TestLifecycle exercises the full Configure → Provision → Validate → Start →
// inbound message → outbound reply → Stop flow using httptest mock servers.
func TestLifecycle(t *testing.T) {
	// Set up a mock Telegram API server.
	var mu sync.Mutex
	var sentMessages []SendMessageRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			writeJSON(t, w, APIResponse[User]{
				OK: true,
				Result: User{
					ID:        111,
					IsBot:     true,
					FirstName: "TestBot",
					Username:  "lifecycle_bot",
				},
			})

		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			var req GetUpdatesRequest
			_ = json.Unmarshal(body, &req)

			mu.Lock()
			count := len(sentMessages)
			mu.Unlock()

			// On the first poll, return one update. After that, return empty.
			if req.Offset == 0 && count == 0 {
				writeJSON(t, w, APIResponse[[]Update]{
					OK: true,
					Result: []Update{
						{
							UpdateID: 1,
							Message: &Message{
								MessageID: 100,
								From:      &User{ID: 42, FirstName: "Alice", Username: "alice"},
								Chat:      Chat{ID: 42, Type: "private"},
								Text:      "ping",
								Date:      int(time.Now().Unix()),
							},
						},
					},
				})
			} else {
				writeJSON(t, w, APIResponse[[]Update]{OK: true, Result: []Update{}})
				// Slow down polling so we don't spin.
				time.Sleep(50 * time.Millisecond)
			}

		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			var req SendMessageRequest
			_ = json.Unmarshal(body, &req)
			mu.Lock()
			sentMessages = append(sentMessages, req)
			mu.Unlock()
			writeJSON(t, w, APIResponse[Message]{
				OK: true,
				Result: Message{
					MessageID: 200,
					Chat:      Chat{ID: req.ChatID, Type: "private"},
					Text:      req.Text,
				},
			})

		case strings.HasSuffix(r.URL.Path, "/sendChatAction"):
			writeJSON(t, w, APIResponse[bool]{OK: true, Result: true})

		default:
			t.Logf("unexpected API call: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// 1. Configure — decode YAML into the module.
	tg := &Telegram{}

	cfgYAML := `
token: "TEST_TOKEN"
mode: "polling"
polling_timeout: 0
allow_users: ["42"]
api_url: "` + srv.URL + `"
`

	var node yaml.Node
	if err := yaml.Unmarshal([]byte(cfgYAML), &node); err != nil {
		t.Fatalf("unmarshal yaml: %v", err)
	}
	// yaml.Unmarshal wraps in a document node; pass the first child.
	if err := tg.Configure(node.Content[0]); err != nil {
		t.Fatalf("Configure() error: %v", err)
	}

	if tg.config.Token != "TEST_TOKEN" {
		t.Errorf("config.Token = %q, want %q", tg.config.Token, "TEST_TOKEN")
	}
	if tg.config.Mode != "polling" {
		t.Errorf("config.Mode = %q, want %q", tg.config.Mode, "polling")
	}

	// 2. Provision — set up client, logger, allowlist.
	appCtx := core.NewAppContext(discardLogger(), t.TempDir(), t.TempDir())
	if err := tg.Provision(appCtx); err != nil {
		t.Fatalf("Provision() error: %v", err)
	}

	if tg.client == nil {
		t.Fatal("client should be set after Provision()")
	}
	if tg.allowList == nil {
		t.Fatal("allowList should be set after Provision()")
	}

	// 3. Validate.
	if err := tg.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}

	// 4. SetInbox — simulate the router wiring.
	var inboxMu sync.Mutex
	var inboxMessages []message.InboundMessage
	tg.SetInbox(func(msg message.InboundMessage) error {
		inboxMu.Lock()
		inboxMessages = append(inboxMessages, msg)
		inboxMu.Unlock()
		return nil
	})

	// 5. Start — this calls getMe + starts polling.
	if err := tg.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Wait for the inbound message to arrive via polling.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		inboxMu.Lock()
		n := len(inboxMessages)
		inboxMu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	inboxMu.Lock()
	if len(inboxMessages) != 1 {
		t.Fatalf("inbox received %d messages, want 1", len(inboxMessages))
	}
	inbound := inboxMessages[0]
	inboxMu.Unlock()

	if inbound.Sender.Username != "alice" {
		t.Errorf("Sender.Username = %q, want %q", inbound.Sender.Username, "alice")
	}
	if inbound.TextContent() != "ping" {
		t.Errorf("TextContent() = %q, want %q", inbound.TextContent(), "ping")
	}

	// 6. Send an outbound reply.
	outbound := message.NewTextMessage(inbound.Chat, "pong")
	if err := tg.Send(context.Background(), outbound); err != nil {
		t.Fatalf("Send() error: %v", err)
	}

	mu.Lock()
	if len(sentMessages) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sentMessages))
	}
	if sentMessages[0].Text != "pong" {
		t.Errorf("sent text = %q, want %q", sentMessages[0].Text, "pong")
	}
	mu.Unlock()

	// 7. Verify typing indicator.
	if err := tg.SendTyping(context.Background(), inbound.Chat); err != nil {
		t.Fatalf("SendTyping() error: %v", err)
	}

	// 8. Verify streaming support.
	if !tg.SupportsStreaming() {
		t.Error("SupportsStreaming() = false, want true")
	}

	// 9. Verify interface compliance.
	var _ channel.Channel = tg
	var _ channel.StreamingChannel = tg
	var _ channel.TypingChannel = tg

	// 10. Stop.
	if err := tg.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
}

// TestModuleRegistered verifies the module is registered via init().
func TestModuleRegistered(t *testing.T) {
	info, ok := core.GetModule("channel.telegram")
	if !ok {
		t.Fatal("channel.telegram module not registered")
	}
	if info.ID != "channel.telegram" {
		t.Errorf("ID = %q, want %q", info.ID, "channel.telegram")
	}
	if info.New == nil {
		t.Fatal("New function is nil")
	}
	mod := info.New()
	if _, ok := mod.(*Telegram); !ok {
		t.Errorf("New() returned %T, want *Telegram", mod)
	}
}

// TestValidateRejectsEmptyToken verifies that Validate fails without a token.
func TestValidateRejectsEmptyToken(t *testing.T) {
	tg := &Telegram{}
	tg.config.defaults()
	tg.config.Token = ""

	if err := tg.Validate(); err == nil {
		t.Error("Validate() should error with empty token")
	}
}

// TestValidateRejectsInvalidMode verifies that Validate rejects unknown modes.
func TestValidateRejectsInvalidMode(t *testing.T) {
	tg := &Telegram{}
	tg.config.Token = "test"
	tg.config.Mode = "invalid"

	if err := tg.Validate(); err == nil {
		t.Error("Validate() should error with invalid mode")
	}
}

// TestValidateWebhookRequiresURL verifies webhook mode needs a URL.
func TestValidateWebhookRequiresURL(t *testing.T) {
	tg := &Telegram{}
	tg.config.Token = "test"
	tg.config.Mode = "webhook"
	tg.config.WebhookURL = ""

	if err := tg.Validate(); err == nil {
		t.Error("Validate() should error when webhook mode has no URL")
	}
}
