package heartbeat

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/flemzord/sclaw/pkg/message"
)

// WebhookMode determines how the webhook processes messages.
type WebhookMode string

const (
	// WebhookModeLazy enqueues the message via Submit (non-blocking).
	WebhookModeLazy WebhookMode = "lazy"
	// WebhookModeEager processes the message synchronously in the HTTP goroutine.
	WebhookModeEager WebhookMode = "eager"
)

// MessageSubmitter enqueues messages for async processing (lazy mode).
type MessageSubmitter interface {
	Submit(msg message.InboundMessage) error
}

// MessageProcessor processes messages synchronously (eager mode).
type MessageProcessor interface {
	Process(ctx context.Context, msg message.InboundMessage) error
}

// WebhookConfig configures the webhook handler.
type WebhookConfig struct {
	Mode      WebhookMode
	Submitter MessageSubmitter // required for lazy mode
	Processor MessageProcessor // required for eager mode
	Logger    *slog.Logger
	Secret    string // optional HMAC-SHA256 secret
}

// WebhookHandler is an http.Handler that receives webhook messages.
type WebhookHandler struct {
	cfg WebhookConfig
}

// NewWebhookHandler creates a WebhookHandler with the given config.
func NewWebhookHandler(cfg WebhookConfig) (*WebhookHandler, error) {
	switch cfg.Mode {
	case WebhookModeLazy:
		if cfg.Submitter == nil {
			return nil, errors.New("heartbeat: lazy webhook requires a MessageSubmitter")
		}
	case WebhookModeEager:
		if cfg.Processor == nil {
			return nil, errors.New("heartbeat: eager webhook requires a MessageProcessor")
		}
	default:
		return nil, fmt.Errorf("heartbeat: unknown webhook mode: %q", cfg.Mode)
	}

	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(nil, nil))
	}

	return &WebhookHandler{cfg: cfg}, nil
}

// ServeHTTP implements http.Handler.
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	// Validate HMAC if secret is configured.
	if h.cfg.Secret != "" {
		sig := r.Header.Get("X-Signature-256")
		if !h.validateHMAC(body, sig) {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	var msg message.InboundMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	switch h.cfg.Mode {
	case WebhookModeLazy:
		if err := h.cfg.Submitter.Submit(msg); err != nil {
			h.cfg.Logger.Error("webhook submit failed", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	case WebhookModeEager:
		if err := h.cfg.Processor.Process(r.Context(), msg); err != nil {
			h.cfg.Logger.Error("webhook process failed", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// validateHMAC checks the HMAC-SHA256 signature.
func (h *WebhookHandler) validateHMAC(body []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(h.cfg.Secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte("sha256="+expected), []byte(signature)) == 1
}
