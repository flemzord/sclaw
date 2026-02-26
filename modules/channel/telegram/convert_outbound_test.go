package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flemzord/sclaw/pkg/message"
)

func TestSendChunk_TextAutoMarkdownV2(t *testing.T) {
	var captured SendMessageRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		writeJSON(t, w, APIResponse[Message]{
			OK:     true,
			Result: Message{MessageID: 1, Chat: Chat{ID: 42, Type: "private"}},
		})
	}))
	defer srv.Close()

	tg := &Telegram{
		client: NewClient("TOKEN", srv.URL),
		logger: discardLogger(),
		config: Config{StreamFlushInterval: 50 * time.Millisecond},
	}

	msg := message.OutboundMessage{
		Chat: message.Chat{ID: "42", Type: message.ChatDM},
		Blocks: []message.ContentBlock{
			{Type: message.BlockText, Text: "Hello **world**!"},
		},
		// Hints is nil — should trigger auto MarkdownV2 conversion.
	}

	if err := tg.sendOutbound(context.Background(), msg); err != nil {
		t.Fatalf("sendOutbound() error: %v", err)
	}

	if captured.ParseMode != "MarkdownV2" {
		t.Errorf("ParseMode = %q, want %q", captured.ParseMode, "MarkdownV2")
	}

	// FormatMarkdownV2 converts **world** → *world* and escapes other chars.
	want := FormatMarkdownV2("Hello **world**!")
	if captured.Text != want {
		t.Errorf("Text = %q, want %q", captured.Text, want)
	}
}

func TestSendChunk_TextExplicitParseMode(t *testing.T) {
	var captured SendMessageRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		writeJSON(t, w, APIResponse[Message]{
			OK:     true,
			Result: Message{MessageID: 1, Chat: Chat{ID: 42, Type: "private"}},
		})
	}))
	defer srv.Close()

	tg := &Telegram{
		client: NewClient("TOKEN", srv.URL),
		logger: discardLogger(),
		config: Config{StreamFlushInterval: 50 * time.Millisecond},
	}

	msg := message.OutboundMessage{
		Chat: message.Chat{ID: "42", Type: message.ChatDM},
		Blocks: []message.ContentBlock{
			{Type: message.BlockText, Text: "<b>bold</b>"},
		},
		Hints: &message.OutboundHints{ParseMode: "HTML"},
	}

	if err := tg.sendOutbound(context.Background(), msg); err != nil {
		t.Fatalf("sendOutbound() error: %v", err)
	}

	if captured.ParseMode != "HTML" {
		t.Errorf("ParseMode = %q, want %q", captured.ParseMode, "HTML")
	}
	if captured.Text != "<b>bold</b>" {
		t.Errorf("Text = %q, want %q", captured.Text, "<b>bold</b>")
	}
}

func TestSendChunk_ImageCaptionAutoMarkdownV2(t *testing.T) {
	var captured SendPhotoRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/sendPhoto") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		writeJSON(t, w, APIResponse[Message]{
			OK:     true,
			Result: Message{MessageID: 1, Chat: Chat{ID: 42, Type: "private"}},
		})
	}))
	defer srv.Close()

	tg := &Telegram{
		client: NewClient("TOKEN", srv.URL),
		logger: discardLogger(),
		config: Config{StreamFlushInterval: 50 * time.Millisecond},
	}

	msg := message.OutboundMessage{
		Chat: message.Chat{ID: "42", Type: message.ChatDM},
		Blocks: []message.ContentBlock{
			{Type: message.BlockImage, URL: "https://example.com/img.png", Caption: "A **nice** photo"},
		},
	}

	if err := tg.sendOutbound(context.Background(), msg); err != nil {
		t.Fatalf("sendOutbound() error: %v", err)
	}

	if captured.ParseMode != "MarkdownV2" {
		t.Errorf("ParseMode = %q, want %q", captured.ParseMode, "MarkdownV2")
	}

	want := FormatMarkdownV2("A **nice** photo")
	if captured.Caption != want {
		t.Errorf("Caption = %q, want %q", captured.Caption, want)
	}
}
