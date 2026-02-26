package anthropic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flemzord/sclaw/internal/provider"
)

func TestHealthCheck_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_health",
			"type": "message",
			"role": "assistant",
			"content": [{"type": "text", "text": "h"}],
			"model": "claude-sonnet-4-5-20250929",
			"stop_reason": "end_turn",
			"stop_sequence": null,
			"usage": {"input_tokens": 3, "output_tokens": 1}
		}`))
	}))
	defer srv.Close()

	a := newTestProvider(srv.URL)

	if err := a.HealthCheck(context.Background()); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestHealthCheck_AuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid api key"}}`))
	}))
	defer srv.Close()

	a := newTestProvider(srv.URL)

	err := a.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Auth errors are not sentinel errors, just wrapped.
	if errors.Is(err, provider.ErrRateLimit) || errors.Is(err, provider.ErrProviderDown) {
		t.Errorf("auth error should not be ErrRateLimit or ErrProviderDown, got %v", err)
	}
}

func TestHealthCheck_RateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`))
	}))
	defer srv.Close()

	a := newTestProvider(srv.URL)

	err := a.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, provider.ErrRateLimit) {
		t.Errorf("expected ErrRateLimit, got %v", err)
	}
}

func TestHealthCheck_ProviderDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}`))
	}))
	defer srv.Close()

	a := newTestProvider(srv.URL)

	err := a.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, provider.ErrProviderDown) {
		t.Errorf("expected ErrProviderDown, got %v", err)
	}
}
