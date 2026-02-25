package gateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
)

// WebhookHandler processes a validated webhook payload.
type WebhookHandler interface {
	HandleWebhook(ctx context.Context, source string, body []byte, headers http.Header) error
}

type webhookEntry struct {
	handler WebhookHandler
	secret  string
}

// WebhookDispatcher routes incoming webhooks to registered handlers with HMAC validation.
type WebhookDispatcher struct {
	mu       sync.RWMutex
	handlers map[string]webhookEntry
	logger   *slog.Logger
}

// NewWebhookDispatcher creates a ready-to-use dispatcher.
func NewWebhookDispatcher(logger *slog.Logger) *WebhookDispatcher {
	return &WebhookDispatcher{
		handlers: make(map[string]webhookEntry),
		logger:   logger,
	}
}

// Register adds a handler for the given source with an optional HMAC secret.
func (d *WebhookDispatcher) Register(source string, h WebhookHandler, secret string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[source] = webhookEntry{handler: h, secret: secret}
}

// ServeHTTP implements http.Handler. It extracts the source from the chi URL param,
// validates HMAC if configured, and dispatches to the registered handler.
func (d *WebhookDispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	source := chi.URLParam(r, "source")
	if source == "" {
		http.Error(w, "missing source", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	d.mu.RLock()
	entry, ok := d.handlers[source]
	d.mu.RUnlock()

	if !ok {
		d.logger.Warn("webhook received for unregistered source", "source", source)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"warning":"no handler registered"}`))
		return
	}

	// Validate HMAC if secret is configured.
	if entry.secret != "" {
		sig := r.Header.Get("X-Signature-256")
		if !validateHMAC(body, sig, entry.secret) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	if err := entry.handler.HandleWebhook(r.Context(), source, body, r.Header); err != nil {
		d.logger.Error("webhook handler failed", "source", source, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// validateHMAC checks HMAC-SHA256 signature in constant time.
func validateHMAC(body []byte, signature, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) == 1
}
