package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func TestGetMe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botTEST_TOKEN/getMe" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}

		writeJSON(t, w, APIResponse[User]{
			OK: true,
			Result: User{
				ID:        123,
				IsBot:     true,
				FirstName: "TestBot",
				Username:  "test_bot",
			},
		})
	}))
	defer srv.Close()

	client := NewClient("TEST_TOKEN", srv.URL)
	user, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("GetMe() error: %v", err)
	}
	if user.ID != 123 {
		t.Errorf("ID = %d, want 123", user.ID)
	}
	if !user.IsBot {
		t.Error("IsBot = false, want true")
	}
	if user.FirstName != "TestBot" {
		t.Errorf("FirstName = %q, want %q", user.FirstName, "TestBot")
	}
	if user.Username != "test_bot" {
		t.Errorf("Username = %q, want %q", user.Username, "test_bot")
	}
}

func TestSendMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botTOKEN/sendMessage" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var req SendMessageRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if req.ChatID != 42 {
			t.Errorf("ChatID = %d, want 42", req.ChatID)
		}
		if req.Text != "hello" {
			t.Errorf("Text = %q, want %q", req.Text, "hello")
		}
		if req.ParseMode != "MarkdownV2" {
			t.Errorf("ParseMode = %q, want %q", req.ParseMode, "MarkdownV2")
		}

		writeJSON(t, w, APIResponse[Message]{
			OK: true,
			Result: Message{
				MessageID: 99,
				Chat:      Chat{ID: 42, Type: "private"},
				Text:      "hello",
			},
		})
	}))
	defer srv.Close()

	client := NewClient("TOKEN", srv.URL)
	msg, err := client.SendMessage(context.Background(), SendMessageRequest{
		ChatID:    42,
		Text:      "hello",
		ParseMode: "MarkdownV2",
	})
	if err != nil {
		t.Fatalf("SendMessage() error: %v", err)
	}
	if msg.MessageID != 99 {
		t.Errorf("MessageID = %d, want 99", msg.MessageID)
	}
	if msg.Text != "hello" {
		t.Errorf("Text = %q, want %q", msg.Text, "hello")
	}
}

func TestGetUpdates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botTOKEN/getUpdates" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var req GetUpdatesRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		if req.Offset != 100 {
			t.Errorf("Offset = %d, want 100", req.Offset)
		}
		if req.Timeout != 30 {
			t.Errorf("Timeout = %d, want 30", req.Timeout)
		}

		writeJSON(t, w, APIResponse[[]Update]{
			OK: true,
			Result: []Update{
				{
					UpdateID: 100,
					Message: &Message{
						MessageID: 1,
						Text:      "test",
						Chat:      Chat{ID: 42, Type: "private"},
					},
				},
				{
					UpdateID: 101,
					Message: &Message{
						MessageID: 2,
						Text:      "test2",
						Chat:      Chat{ID: 42, Type: "private"},
					},
				},
			},
		})
	}))
	defer srv.Close()

	client := NewClient("TOKEN", srv.URL)
	updates, err := client.GetUpdates(context.Background(), GetUpdatesRequest{
		Offset:  100,
		Timeout: 30,
	})
	if err != nil {
		t.Fatalf("GetUpdates() error: %v", err)
	}
	if len(updates) != 2 {
		t.Fatalf("len(updates) = %d, want 2", len(updates))
	}
	if updates[0].UpdateID != 100 {
		t.Errorf("updates[0].UpdateID = %d, want 100", updates[0].UpdateID)
	}
	if updates[1].Message.Text != "test2" {
		t.Errorf("updates[1].Message.Text = %q, want %q", updates[1].Message.Text, "test2")
	}
}

func TestRateLimitRetry(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			// First call: 429 with retry_after.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			writeJSON(t, w, APIResponse[json.RawMessage]{
				OK:          false,
				ErrorCode:   429,
				Description: "Too Many Requests: retry after 1",
				Parameters:  &ResponseParameters{RetryAfter: 1},
			})
			return
		}
		// Second call: success.
		writeJSON(t, w, APIResponse[User]{
			OK: true,
			Result: User{
				ID:        456,
				IsBot:     true,
				FirstName: "RetryBot",
			},
		})
	}))
	defer srv.Close()

	client := NewClient("TOKEN", srv.URL)
	user, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("GetMe() error after retry: %v", err)
	}
	if user.ID != 456 {
		t.Errorf("ID = %d, want 456", user.ID)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2", got)
	}
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, APIResponse[json.RawMessage]{
			OK:          false,
			ErrorCode:   400,
			Description: "Bad Request: chat not found",
		})
	}))
	defer srv.Close()

	client := NewClient("TOKEN", srv.URL)
	_, err := client.SendMessage(context.Background(), SendMessageRequest{
		ChatID: 999,
		Text:   "hello",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != 400 {
		t.Errorf("Code = %d, want 400", apiErr.Code)
	}
	if apiErr.Description != "Bad Request: chat not found" {
		t.Errorf("Description = %q, want %q", apiErr.Description, "Bad Request: chat not found")
	}
}

func TestFileURL(t *testing.T) {
	client := NewClient("TOKEN", "https://api.telegram.org")
	got := client.FileURL("documents/file_123.pdf")
	want := "https://api.telegram.org/file/botTOKEN/documents/file_123.pdf"
	if got != want {
		t.Errorf("FileURL() = %q, want %q", got, want)
	}
}

func TestConfigDefaults(t *testing.T) {
	var cfg Config
	cfg.defaults()

	if cfg.Mode != "polling" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "polling")
	}
	if cfg.PollingTimeout != 30 {
		t.Errorf("PollingTimeout = %d, want 30", cfg.PollingTimeout)
	}
	if len(cfg.AllowedUpdates) != 3 {
		t.Errorf("len(AllowedUpdates) = %d, want 3", len(cfg.AllowedUpdates))
	}
	if cfg.MaxMessageLength != 4096 {
		t.Errorf("MaxMessageLength = %d, want 4096", cfg.MaxMessageLength)
	}
	if cfg.StreamFlushInterval.Seconds() != 1 {
		t.Errorf("StreamFlushInterval = %v, want 1s", cfg.StreamFlushInterval)
	}
	if cfg.APIURL != "https://api.telegram.org" {
		t.Errorf("APIURL = %q, want %q", cfg.APIURL, "https://api.telegram.org")
	}
}

func TestConfigDefaultsPreservesValues(t *testing.T) {
	cfg := Config{
		Mode:           "webhook",
		PollingTimeout: 60,
		APIURL:         "https://custom.api.example.com",
	}
	cfg.defaults()

	if cfg.Mode != "webhook" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "webhook")
	}
	if cfg.PollingTimeout != 60 {
		t.Errorf("PollingTimeout = %d, want 60", cfg.PollingTimeout)
	}
	if cfg.APIURL != "https://custom.api.example.com" {
		t.Errorf("APIURL = %q, want %q", cfg.APIURL, "https://custom.api.example.com")
	}
}

func TestAPIErrorMessage(t *testing.T) {
	err := &APIError{Code: 429, Description: "Too Many Requests", RetryAfter: 5}
	want := "telegram: 429 Too Many Requests (retry after 5s)"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	err2 := &APIError{Code: 400, Description: "Bad Request"}
	want2 := "telegram: 400 Bad Request"
	if got := err2.Error(); got != want2 {
		t.Errorf("Error() = %q, want %q", got, want2)
	}
}
