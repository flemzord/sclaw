package openairesponses

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/flemzord/sclaw/internal/provider"
)

// connManager manages WebSocket connections to the Responses API.
//
// The coder/websocket library closes the connection on any error, including
// context cancellation (see Conn doc: "This applies to context expirations
// as well unfortunately"). This makes connection reuse impractical:
// there is no way to stop an idle Read without closing the connection.
//
// Instead, each request dials a fresh connection. The ~200ms dial overhead
// is negligible compared to LLM response latency.
type connManager struct {
	cfg    Config
	logger *slog.Logger

	mu     sync.Mutex
	closed bool
}

// newConnManager creates a new connection manager.
func newConnManager(cfg Config, logger *slog.Logger) *connManager {
	return &connManager{
		cfg:    cfg,
		logger: logger,
	}
}

// getConn returns a fresh WebSocket connection for one request.
// Connections are never reused because coder/websocket closes on cancelled reads.
func (cm *connManager) getConn(ctx context.Context) (*websocket.Conn, error) {
	cm.mu.Lock()
	if cm.closed {
		cm.mu.Unlock()
		return nil, fmt.Errorf("%w: connection manager is closed", provider.ErrProviderDown)
	}
	cm.mu.Unlock()
	return cm.dial(ctx)
}

// Close permanently shuts down the connection manager.
func (cm *connManager) Close() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.closed = true
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
