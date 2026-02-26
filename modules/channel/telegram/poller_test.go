package telegram

import (
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
		// Second call: empty (give poller time to stop).
		writeJSON(t, w, APIResponse[[]Update]{OK: true, Result: []Update{}})
		// Sleep to let stop signal propagate.
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
		PollingTimeout: 0, // No long-polling timeout in tests.
		AllowedUpdates: []string{"message"},
	})

	poller.Start()
	// Wait for at least one update to be processed.
	time.Sleep(500 * time.Millisecond)
	poller.Stop()

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
	allowList := channel.NewAllowList([]string{"100"}, nil) // Only user 100 is allowed.

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
	poller.Stop()

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
		// Always return error.
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
	// Give it enough time to hit the circuit breaker (5 errors).
	time.Sleep(300 * time.Millisecond)
	poller.Stop()

	// Should have hit at least 5 errors to trigger the breaker.
	if got := calls.Load(); got < 5 {
		t.Errorf("calls = %d, want >= 5", got)
	}
}
