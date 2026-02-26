package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/channel"
	"github.com/flemzord/sclaw/pkg/message"
)

func TestPollerReceivesUpdates(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := callCount.Add(1)
		if n == 1 {
			writeJSON(t, w, APIResponse[[]Update]{
				OK: true,
				Result: []Update{
					{
						UpdateID: 1,
						Message: &Message{
							MessageID: 10,
							From:      &User{ID: 100, FirstName: "Alice", Username: "alice"},
							Chat:      Chat{ID: 200, Type: "private"},
							Text:      "hello",
							Date:      1700000000,
						},
					},
				},
			})
			return
		}
		writeJSON(t, w, APIResponse[[]Update]{OK: true, Result: []Update{}})
		time.Sleep(100 * time.Millisecond)
	}))
	defer srv.Close()

	client := NewClient("TOKEN", srv.URL)
	allowList := channel.NewAllowList([]string{"100"}, nil)

	var mu sync.Mutex
	var received []message.InboundMessage

	poller := NewPoller(client, func(msg message.InboundMessage) error {
		mu.Lock()
		received = append(received, msg)
		mu.Unlock()
		return nil
	}, allowList, discardLogger(), "test_bot", "telegram", Config{
		PollingTimeout: 0,
		AllowedUpdates: []string{"message"},
	})

	poller.Start()
	time.Sleep(500 * time.Millisecond)
	if err := poller.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("received %d messages, want 1", len(received))
	}
	if received[0].Sender.ID != "100" {
		t.Errorf("Sender.ID = %q, want %q", received[0].Sender.ID, "100")
	}
}

func TestPollerDeniesUnallowedUsers(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := callCount.Add(1)
		if n == 1 {
			writeJSON(t, w, APIResponse[[]Update]{
				OK: true,
				Result: []Update{
					{
						UpdateID: 1,
						Message: &Message{
							MessageID: 10,
							From:      &User{ID: 999, FirstName: "Eve"},
							Chat:      Chat{ID: 200, Type: "private"},
							Text:      "hack",
							Date:      1700000000,
						},
					},
				},
			})
			return
		}
		writeJSON(t, w, APIResponse[[]Update]{OK: true, Result: []Update{}})
		time.Sleep(100 * time.Millisecond)
	}))
	defer srv.Close()

	client := NewClient("TOKEN", srv.URL)
	allowList := channel.NewAllowList([]string{"100"}, nil)

	var mu sync.Mutex
	var received []message.InboundMessage

	poller := NewPoller(client, func(msg message.InboundMessage) error {
		mu.Lock()
		received = append(received, msg)
		mu.Unlock()
		return nil
	}, allowList, discardLogger(), "test_bot", "telegram", Config{
		PollingTimeout: 0,
		AllowedUpdates: []string{"message"},
	})

	poller.Start()
	time.Sleep(500 * time.Millisecond)
	_ = poller.Stop(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 0 {
		t.Errorf("received %d messages, want 0 (denied)", len(received))
	}
}

func TestPollerCircuitBreaker(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeJSON(t, w, APIResponse[json.RawMessage]{
			OK:          false,
			ErrorCode:   500,
			Description: "Internal Server Error",
		})
	}))
	defer srv.Close()

	client := NewClient("TOKEN", srv.URL)
	allowList := channel.NewAllowList([]string{"100"}, nil)

	poller := NewPoller(client, func(_ message.InboundMessage) error {
		return nil
	}, allowList, discardLogger(), "test_bot", "telegram", Config{
		PollingTimeout: 0,
		AllowedUpdates: []string{"message"},
	})

	poller.Start()
	time.Sleep(2 * time.Second)
	_ = poller.Stop(context.Background())

	if got := calls.Load(); got < 2 {
		t.Errorf("calls = %d, want >= 2 (backoff reduces frequency)", got)
	}
}

func TestPollerIdempotentStop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, APIResponse[[]Update]{OK: true, Result: []Update{}})
	}))
	defer srv.Close()

	client := NewClient("TOKEN", srv.URL)
	poller := NewPoller(client, func(_ message.InboundMessage) error {
		return nil
	}, channel.NewAllowList(nil, nil), discardLogger(), "test_bot", "telegram", Config{
		PollingTimeout: 0,
		AllowedUpdates: []string{"message"},
	})

	poller.Start()
	time.Sleep(100 * time.Millisecond)

	// Should not panic on double stop.
	_ = poller.Stop(context.Background())
	_ = poller.Stop(context.Background())
}
