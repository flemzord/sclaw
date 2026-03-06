package openairesponses

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestConnManager_IdleReaderHandlesPings verifies that the idle reader
// goroutine keeps the connection alive by responding to server pings.
// coder/websocket's Ping() must be called concurrently with Read() on the
// same connection (the Read call processes the incoming pong frame).
func TestConnManager_IdleReaderHandlesPings(t *testing.T) {
	var pongCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
		if err != nil {
			t.Logf("accept: %v", err)
			return
		}
		defer conn.CloseNow() //nolint:errcheck

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Server-side Read loop so that Ping can receive pongs.
		go func() {
			for {
				_, _, readErr := conn.Read(ctx)
				if readErr != nil {
					return
				}
			}
		}()

		// Send pings and count successful pong responses.
		for i := 0; i < 3; i++ {
			pingCtx, pingCancel := context.WithTimeout(ctx, 2*time.Second)
			if pingErr := conn.Ping(pingCtx); pingErr != nil {
				t.Logf("ping %d failed: %v", i, pingErr)
				pingCancel()
				return
			}
			pingCancel()
			pongCount.Add(1)
			time.Sleep(20 * time.Millisecond)
		}

		// Wait a bit then close gracefully.
		time.Sleep(50 * time.Millisecond)
		conn.Close(websocket.StatusNormalClosure, "done") //nolint:errcheck
	}))
	t.Cleanup(srv.Close)

	cfg := testConfig(wsURL(srv))
	cm := newConnManager(cfg, testLogger())
	defer cm.Close() //nolint:errcheck

	// Acquire and immediately release to trigger idle reader.
	conn, err := cm.getConn(context.Background())
	if err != nil {
		t.Fatalf("getConn: %v", err)
	}
	if conn == nil {
		t.Fatal("getConn returned nil connection")
	}
	cm.release()

	// Wait for pings to complete.
	time.Sleep(500 * time.Millisecond)

	if got := pongCount.Load(); got < 3 {
		t.Errorf("pong count = %d, want >= 3 (idle reader should respond to pings)", got)
	}
}

// TestConnManager_IdleReaderDetectsClose verifies that when the server
// closes the connection while idle, the connManager invalidates it.
func TestConnManager_IdleReaderDetectsClose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
		if err != nil {
			t.Logf("accept: %v", err)
			return
		}
		// Close immediately after a short delay to simulate server-side close.
		time.Sleep(50 * time.Millisecond)
		conn.Close(websocket.StatusInternalError, "keepalive ping timeout") //nolint:errcheck
	}))
	t.Cleanup(srv.Close)

	cfg := testConfig(wsURL(srv))
	cm := newConnManager(cfg, testLogger())
	defer cm.Close() //nolint:errcheck

	// Acquire and release to start idle reader.
	_, err := cm.getConn(context.Background())
	if err != nil {
		t.Fatalf("getConn: %v", err)
	}
	cm.release()

	// Wait for the server close to propagate.
	time.Sleep(200 * time.Millisecond)

	// The connection should have been invalidated by the idle reader.
	cm.mu.Lock()
	connIsNil := cm.conn == nil
	cm.mu.Unlock()

	if !connIsNil {
		t.Error("expected connection to be nil after server close, but it's still set")
	}

	// A subsequent getConn should succeed (dials a new connection).
	// We need a new server handler that accepts and stays open.
	// Since the original server is still running, it will accept new connections.
	conn2, err := cm.getConn(context.Background())
	if err != nil {
		t.Fatalf("second getConn: %v", err)
	}
	if conn2 == nil {
		t.Fatal("second getConn returned nil")
	}
	cm.release()
}
