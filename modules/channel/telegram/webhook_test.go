package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/flemzord/sclaw/internal/channel"
	"github.com/flemzord/sclaw/pkg/message"
)

func TestWebhookValidSecret(t *testing.T) {
	var received []message.InboundMessage
	wh := NewWebhookReceiver(nil, func(msg message.InboundMessage) error {
		received = append(received, msg)
		return nil
	}, channel.NewAllowList([]string{"123"}, nil), discardLogger(), "testbot", "telegram", "my-secret")

	update := Update{
		UpdateID: 1,
		Message: &Message{
			MessageID: 42,
			From:      &User{ID: 123, FirstName: "Alice"},
			Chat:      Chat{ID: 456, Type: "private"},
			Date:      1700000000,
			Text:      "hello",
		},
	}
	body, _ := json.Marshal(update)

	headers := http.Header{}
	headers.Set("X-Telegram-Bot-Api-Secret-Token", "my-secret")

	err := wh.HandleWebhook(context.TODO(), "telegram", body, headers)
	if err != nil {
		t.Fatalf("HandleWebhook() error: %v", err)
	}
	if len(received) != 1 {
		t.Fatalf("received %d messages, want 1", len(received))
	}
	if received[0].Sender.ID != "123" {
		t.Errorf("Sender.ID = %q, want %q", received[0].Sender.ID, "123")
	}
}

func TestWebhookInvalidSecret(t *testing.T) {
	wh := NewWebhookReceiver(nil, func(_ message.InboundMessage) error {
		t.Error("inbox should not be called for invalid secret")
		return nil
	}, channel.NewAllowList(nil, nil), discardLogger(), "testbot", "telegram", "my-secret")

	update := Update{
		UpdateID: 1,
		Message: &Message{
			MessageID: 42,
			From:      &User{ID: 123, FirstName: "Alice"},
			Chat:      Chat{ID: 456, Type: "private"},
			Date:      1700000000,
			Text:      "hello",
		},
	}
	body, _ := json.Marshal(update)

	headers := http.Header{}
	headers.Set("X-Telegram-Bot-Api-Secret-Token", "wrong-secret")

	err := wh.HandleWebhook(context.TODO(), "telegram", body, headers)
	if err == nil {
		t.Fatal("HandleWebhook() should error with invalid secret")
	}
}

func TestWebhookNoSecret(t *testing.T) {
	var received []message.InboundMessage
	wh := NewWebhookReceiver(nil, func(msg message.InboundMessage) error {
		received = append(received, msg)
		return nil
	}, channel.NewAllowList([]string{"123"}, nil), discardLogger(), "testbot", "telegram", "")

	update := Update{
		UpdateID: 1,
		Message: &Message{
			MessageID: 42,
			From:      &User{ID: 123, FirstName: "Alice"},
			Chat:      Chat{ID: 456, Type: "private"},
			Date:      1700000000,
			Text:      "hello",
		},
	}
	body, _ := json.Marshal(update)

	// No secret header — should be accepted when secret is not configured.
	err := wh.HandleWebhook(context.TODO(), "telegram", body, http.Header{})
	if err != nil {
		t.Fatalf("HandleWebhook() error: %v", err)
	}
	if len(received) != 1 {
		t.Fatalf("received %d messages, want 1", len(received))
	}
}

func TestWebhookInvalidJSON(t *testing.T) {
	wh := NewWebhookReceiver(nil, func(_ message.InboundMessage) error {
		t.Error("inbox should not be called for invalid JSON")
		return nil
	}, channel.NewAllowList(nil, nil), discardLogger(), "testbot", "telegram", "")

	err := wh.HandleWebhook(context.TODO(), "telegram", []byte(`{invalid json`), http.Header{})
	if err == nil {
		t.Fatal("HandleWebhook() should error with invalid JSON")
	}
}

func TestWebhookAllowListDenied(t *testing.T) {
	var received []message.InboundMessage
	// Only allow user 999 — user 123 should be denied.
	wh := NewWebhookReceiver(nil, func(msg message.InboundMessage) error {
		received = append(received, msg)
		return nil
	}, channel.NewAllowList([]string{"999"}, nil), discardLogger(), "testbot", "telegram", "")

	update := Update{
		UpdateID: 1,
		Message: &Message{
			MessageID: 42,
			From:      &User{ID: 123, FirstName: "Alice"},
			Chat:      Chat{ID: 456, Type: "private"},
			Date:      1700000000,
			Text:      "hello",
		},
	}
	body, _ := json.Marshal(update)

	err := wh.HandleWebhook(context.TODO(), "telegram", body, http.Header{})
	if err != nil {
		t.Fatalf("HandleWebhook() error: %v", err)
	}
	// Message should be denied silently (no error, but not delivered).
	if len(received) != 0 {
		t.Errorf("received %d messages, want 0 (denied)", len(received))
	}
}

func TestWebhookEmptyUpdate(t *testing.T) {
	wh := NewWebhookReceiver(nil, func(_ message.InboundMessage) error {
		t.Error("inbox should not be called for empty update")
		return nil
	}, channel.NewAllowList(nil, nil), discardLogger(), "testbot", "telegram", "")

	update := Update{UpdateID: 1} // No message, edited_message, or channel_post.
	body, _ := json.Marshal(update)

	// Empty update should be skipped silently (no error).
	err := wh.HandleWebhook(context.TODO(), "telegram", body, http.Header{})
	if err != nil {
		t.Fatalf("HandleWebhook() error: %v (empty updates should be skipped)", err)
	}
}
