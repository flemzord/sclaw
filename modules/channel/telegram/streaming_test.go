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

	"github.com/flemzord/sclaw/pkg/message"
)

func TestSendStream(t *testing.T) {
	var mu sync.Mutex
	var edits []string
	var sendCount int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			mu.Lock()
			sendCount++
			mu.Unlock()
			writeJSON(t, w, APIResponse[Message]{
				OK: true,
				Result: Message{
					MessageID: 42,
					Chat:      Chat{ID: 100, Type: "private"},
				},
			})
			return
		}

		if strings.HasSuffix(r.URL.Path, "/editMessageText") {
			var req EditMessageTextRequest
			if err := json.Unmarshal(body, &req); err != nil {
				t.Errorf("unmarshal edit request: %v", err)
			}
			mu.Lock()
			edits = append(edits, req.Text)
			mu.Unlock()
			writeJSON(t, w, APIResponse[Message]{
				OK: true,
				Result: Message{
					MessageID: 42,
					Chat:      Chat{ID: 100, Type: "private"},
					Text:      req.Text,
				},
			})
			return
		}

		t.Errorf("unexpected path: %s", r.URL.Path)
	}))
	defer srv.Close()

	client := NewClient("TOKEN", srv.URL)

	tg := &Telegram{
		client: client,
		logger: discardLogger(),
		config: Config{
			StreamFlushInterval: 50 * time.Millisecond,
		},
	}

	stream := make(chan string, 10)
	stream <- "Hello "
	stream <- "World"
	close(stream)

	chat := message.Chat{ID: "100", Type: message.ChatDM}
	err := tg.SendStream(context.Background(), chat, stream)
	if err != nil {
		t.Fatalf("SendStream() error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if sendCount != 1 {
		t.Errorf("sendMessage calls = %d, want 1", sendCount)
	}

	// Should have at least one edit with the final text.
	if len(edits) == 0 {
		t.Fatal("expected at least one editMessageText call")
	}

	lastEdit := edits[len(edits)-1]
	if lastEdit != "Hello World" {
		t.Errorf("last edit = %q, want %q", lastEdit, "Hello World")
	}
}

func TestSendStreamIgnoresNotModified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			writeJSON(t, w, APIResponse[Message]{
				OK: true,
				Result: Message{
					MessageID: 42,
					Chat:      Chat{ID: 100, Type: "private"},
				},
			})
			return
		}

		if strings.HasSuffix(r.URL.Path, "/editMessageText") {
			// Return "message is not modified" error.
			writeJSON(t, w, APIResponse[json.RawMessage]{
				OK:          false,
				ErrorCode:   400,
				Description: "Bad Request: message is not modified",
			})
			return
		}
	}))
	defer srv.Close()

	client := NewClient("TOKEN", srv.URL)
	tg := &Telegram{
		client: client,
		logger: discardLogger(),
		config: Config{
			StreamFlushInterval: 10 * time.Millisecond,
		},
	}

	stream := make(chan string, 1)
	stream <- "test"
	close(stream)

	chat := message.Chat{ID: "100", Type: message.ChatDM}
	// Should not fail even though all edits return "not modified".
	err := tg.SendStream(context.Background(), chat, stream)
	if err != nil {
		t.Fatalf("SendStream() should not error on 'not modified': %v", err)
	}
}
