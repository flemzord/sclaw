package heartbeat

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/flemzord/sclaw/pkg/message"
)

// mockSubmitter implements MessageSubmitter for testing.
type mockSubmitter struct {
	mu       sync.Mutex
	messages []message.InboundMessage
	err      error
}

func (m *mockSubmitter) Submit(msg message.InboundMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	return m.err
}

func (m *mockSubmitter) submitted() []message.InboundMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	dst := make([]message.InboundMessage, len(m.messages))
	copy(dst, m.messages)
	return dst
}

// mockProcessor implements MessageProcessor for testing.
type mockProcessor struct {
	mu       sync.Mutex
	messages []message.InboundMessage
	err      error
}

func (m *mockProcessor) Process(_ context.Context, msg message.InboundMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	return m.err
}

func (m *mockProcessor) processed() []message.InboundMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	dst := make([]message.InboundMessage, len(m.messages))
	copy(dst, m.messages)
	return dst
}

func TestWebhookHandler_LazyMode(t *testing.T) {
	t.Parallel()

	sub := &mockSubmitter{}
	h, err := NewWebhookHandler(WebhookConfig{
		Mode:      WebhookModeLazy,
		Submitter: sub,
	})
	if err != nil {
		t.Fatalf("NewWebhookHandler: %v", err)
	}

	body := `{"id":"msg-1","channel":"slack"}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	msgs := sub.submitted()
	if len(msgs) != 1 {
		t.Fatalf("submitted %d messages, want 1", len(msgs))
	}
	if msgs[0].ID != "msg-1" {
		t.Errorf("message ID = %q, want %q", msgs[0].ID, "msg-1")
	}
}

func TestWebhookHandler_EagerMode(t *testing.T) {
	t.Parallel()

	proc := &mockProcessor{}
	h, err := NewWebhookHandler(WebhookConfig{
		Mode:      WebhookModeEager,
		Processor: proc,
	})
	if err != nil {
		t.Fatalf("NewWebhookHandler: %v", err)
	}

	body := `{"id":"msg-2","channel":"discord"}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	msgs := proc.processed()
	if len(msgs) != 1 {
		t.Fatalf("processed %d messages, want 1", len(msgs))
	}
	if msgs[0].ID != "msg-2" {
		t.Errorf("message ID = %q, want %q", msgs[0].ID, "msg-2")
	}
}

func TestWebhookHandler_InvalidJSON(t *testing.T) {
	t.Parallel()

	sub := &mockSubmitter{}
	h, err := NewWebhookHandler(WebhookConfig{
		Mode:      WebhookModeLazy,
		Submitter: sub,
	})
	if err != nil {
		t.Fatalf("NewWebhookHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("not json"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestWebhookHandler_WrongMethod(t *testing.T) {
	t.Parallel()

	sub := &mockSubmitter{}
	h, err := NewWebhookHandler(WebhookConfig{
		Mode:      WebhookModeLazy,
		Submitter: sub,
	})
	if err != nil {
		t.Fatalf("NewWebhookHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestWebhookHandler_InvalidSignature(t *testing.T) {
	t.Parallel()

	sub := &mockSubmitter{}
	h, err := NewWebhookHandler(WebhookConfig{
		Mode:      WebhookModeLazy,
		Submitter: sub,
		Secret:    "my-secret",
	})
	if err != nil {
		t.Fatalf("NewWebhookHandler: %v", err)
	}

	body := `{"id":"msg-3"}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	req.Header.Set("X-Signature-256", "sha256=invalidsignature")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestWebhookHandler_ValidSignature(t *testing.T) {
	t.Parallel()

	secret := "my-secret"
	sub := &mockSubmitter{}
	h, err := NewWebhookHandler(WebhookConfig{
		Mode:      WebhookModeLazy,
		Submitter: sub,
		Secret:    secret,
	})
	if err != nil {
		t.Fatalf("NewWebhookHandler: %v", err)
	}

	body := `{"id":"msg-4","channel":"slack"}`

	// Compute the valid HMAC signature.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	req.Header.Set("X-Signature-256", sig)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	msgs := sub.submitted()
	if len(msgs) != 1 {
		t.Fatalf("submitted %d messages, want 1", len(msgs))
	}
	if msgs[0].ID != "msg-4" {
		t.Errorf("message ID = %q, want %q", msgs[0].ID, "msg-4")
	}
}

func TestNewWebhookHandler_MissingSubmitter(t *testing.T) {
	t.Parallel()

	_, err := NewWebhookHandler(WebhookConfig{
		Mode: WebhookModeLazy,
		// Submitter intentionally nil.
	})
	if err == nil {
		t.Fatal("expected error for lazy mode with nil submitter")
	}
}

func TestNewWebhookHandler_MissingProcessor(t *testing.T) {
	t.Parallel()

	_, err := NewWebhookHandler(WebhookConfig{
		Mode: WebhookModeEager,
		// Processor intentionally nil.
	})
	if err == nil {
		t.Fatal("expected error for eager mode with nil processor")
	}
}

func TestNewWebhookHandler_UnknownMode(t *testing.T) {
	t.Parallel()

	_, err := NewWebhookHandler(WebhookConfig{
		Mode: "unknown",
	})
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
	if !errors.Is(err, nil) && !strings.Contains(err.Error(), "unknown webhook mode") {
		t.Errorf("unexpected error: %v", err)
	}
}
