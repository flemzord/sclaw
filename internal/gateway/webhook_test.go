package gateway

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// mockWebhookHandler is a test helper that records calls.
type mockWebhookHandler struct {
	called  bool
	source  string
	body    []byte
	headers http.Header
	err     error
}

func (m *mockWebhookHandler) HandleWebhook(_ context.Context, source string, body []byte, headers http.Header) error {
	m.called = true
	m.source = source
	m.body = body
	m.headers = headers
	return m.err
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

func signPayload(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWebhookDispatcher_RegisteredSource_ValidHMAC(t *testing.T) {
	t.Parallel()

	handler := &mockWebhookHandler{}
	d := NewWebhookDispatcher(testLogger())
	d.Register("github", handler, "my-secret")

	body := []byte(`{"action":"push"}`)
	sig := signPayload(body, "my-secret")

	r := chi.NewRouter()
	r.Post("/webhooks/{source}", d.ServeHTTP)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-Signature-256", sig)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !handler.called {
		t.Error("handler was not called")
	}
	if handler.source != "github" {
		t.Errorf("source = %q, want %q", handler.source, "github")
	}
	if string(handler.body) != string(body) {
		t.Errorf("body = %q, want %q", handler.body, body)
	}
}

func TestWebhookDispatcher_UnregisteredSource(t *testing.T) {
	t.Parallel()

	d := NewWebhookDispatcher(testLogger())

	r := chi.NewRouter()
	r.Post("/webhooks/{source}", d.ServeHTTP)

	body := []byte(`{"data":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/unknown", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (unregistered source returns 404)", rr.Code, http.StatusNotFound)
	}
}

func TestWebhookDispatcher_InvalidHMAC(t *testing.T) {
	t.Parallel()

	handler := &mockWebhookHandler{}
	d := NewWebhookDispatcher(testLogger())
	d.Register("github", handler, "my-secret")

	r := chi.NewRouter()
	r.Post("/webhooks/{source}", d.ServeHTTP)

	body := []byte(`{"data":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-Signature-256", "sha256=invalid")
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
	if handler.called {
		t.Error("handler should not be called with invalid HMAC")
	}
}

func TestWebhookDispatcher_WrongMethod(t *testing.T) {
	t.Parallel()

	d := NewWebhookDispatcher(testLogger())

	r := chi.NewRouter()
	r.Post("/webhooks/{source}", d.ServeHTTP)

	req := httptest.NewRequest(http.MethodGet, "/webhooks/test", nil)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	// chi won't route GET to a POST handler → 405
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestWebhookDispatcher_NoSecretConfigured(t *testing.T) {
	t.Parallel()

	handler := &mockWebhookHandler{}
	d := NewWebhookDispatcher(testLogger())
	d.Register("open", handler, "") // no secret

	r := chi.NewRouter()
	r.Post("/webhooks/{source}", d.ServeHTTP)

	body := []byte(`{"data":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/open", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !handler.called {
		t.Error("handler should be called without secret requirement")
	}
}

func TestWebhookDispatcher_HandlerError(t *testing.T) {
	t.Parallel()

	handler := &mockWebhookHandler{err: errors.New("handler failed")}
	d := NewWebhookDispatcher(testLogger())
	d.Register("failing", handler, "")

	r := chi.NewRouter()
	r.Post("/webhooks/{source}", d.ServeHTTP)

	body := []byte(`{"data":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/failing", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

func TestValidateHMAC(t *testing.T) {
	t.Parallel()

	body := []byte("test payload")
	secret := "test-secret"

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !validateHMAC(body, validSig, secret) {
		t.Error("valid HMAC should pass")
	}
	if validateHMAC(body, "sha256=invalid", secret) {
		t.Error("invalid HMAC should fail")
	}
	if validateHMAC(body, "", secret) {
		t.Error("empty signature should fail")
	}
}
