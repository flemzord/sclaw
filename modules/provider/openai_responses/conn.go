package openairesponses

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/flemzord/sclaw/internal/provider"
)

// connManager manages a single WebSocket connection with lazy dial,
// automatic reconnection on stale/closed connections, and max-age lifecycle.
//
// The Responses API allows only one response per connection at a time,
// and the ReAct loop is sequential, so no pooling is needed.
type connManager struct {
	cfg    Config
	logger *slog.Logger

	mu      sync.Mutex
	conn    *websocket.Conn
	created time.Time
	inUse   bool
	closed  bool
}

// newConnManager creates a new connection manager.
func newConnManager(cfg Config, logger *slog.Logger) *connManager {
	return &connManager{
		cfg:    cfg,
		logger: logger,
	}
}

// getConn returns a valid WebSocket connection, dialing a new one if necessary.
// It marks the connection as in-use; callers must call release() when done.
func (cm *connManager) getConn(ctx context.Context) (*websocket.Conn, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.closed {
		return nil, fmt.Errorf("%w: connection manager is closed", provider.ErrProviderDown)
	}

	if cm.inUse {
		return nil, fmt.Errorf("%w: connection already in use", provider.ErrProviderDown)
	}

	// Recycle stale connections.
	if cm.conn != nil && time.Since(cm.created) > cm.cfg.ConnMaxAge {
		cm.logger.Debug("recycling stale WebSocket connection", "age", time.Since(cm.created))
		cm.conn.CloseNow() //nolint:errcheck // best-effort close
		cm.conn = nil
	}

	// Dial a new connection if needed.
	if cm.conn == nil {
		conn, err := cm.dial(ctx)
		if err != nil {
			return nil, err
		}
		cm.conn = conn
		cm.created = time.Now()
	}

	cm.inUse = true
	return cm.conn, nil
}

// release marks the connection as available for reuse.
func (cm *connManager) release() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.inUse = false
}

// invalidate closes the current connection so a fresh one is dialed next time.
func (cm *connManager) invalidate() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.conn != nil {
		cm.conn.CloseNow() //nolint:errcheck // best-effort close
		cm.conn = nil
	}
	cm.inUse = false
}

// Close permanently shuts down the connection manager.
func (cm *connManager) Close() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.closed = true
	if cm.conn != nil {
		err := cm.conn.Close(websocket.StatusNormalClosure, "shutdown")
		cm.conn = nil
		return err
	}
	return nil
}

// dial establishes a new WebSocket connection to the Responses API.
func (cm *connManager) dial(ctx context.Context) (*websocket.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, cm.cfg.DialTimeout)
	defer cancel()

	header := http.Header{}
	header.Set("Authorization", "Bearer "+cm.cfg.APIKey)
	for k, v := range cm.cfg.Headers {
		header.Set(k, v)
	}

	conn, resp, err := websocket.Dial(dialCtx, cm.cfg.WSEndpoint, &websocket.DialOptions{
		HTTPHeader: header,
	})
	if err != nil {
		if resp != nil {
			return nil, classifyDialError(resp.StatusCode, err)
		}
		return nil, fmt.Errorf("%w: WebSocket dial: %w", provider.ErrProviderDown, err)
	}

	// Set a generous read limit for large model responses.
	conn.SetReadLimit(4 * 1024 * 1024) // 4 MiB

	cm.logger.Debug("WebSocket connection established", "endpoint", cm.cfg.WSEndpoint)
	return conn, nil
}

// classifyDialError maps HTTP handshake status codes to provider sentinel errors.
func classifyDialError(statusCode int, err error) error {
	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return fmt.Errorf("%w: HTTP %d: %w", provider.ErrAuthentication, statusCode, err)
	case statusCode == http.StatusTooManyRequests:
		return fmt.Errorf("%w: HTTP %d: %w", provider.ErrRateLimit, statusCode, err)
	case statusCode >= 500:
		return fmt.Errorf("%w: HTTP %d: %w", provider.ErrProviderDown, statusCode, err)
	default:
		return fmt.Errorf("%w: HTTP %d: %w", provider.ErrProviderDown, statusCode, err)
	}
}
