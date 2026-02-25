package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/router"
)

func TestHealth_AllHealthy(t *testing.T) {
	t.Parallel()

	store := router.NewInMemorySessionStore()
	store.GetOrCreate(router.SessionKey{Channel: "test", ChatID: "1"})

	chain := newTestChain(t, []provider.ChainEntry{
		{Name: "p1", Provider: &fakeProvider{name: "p1"}, Role: provider.RolePrimary},
	})

	g := &Gateway{
		sessions: store,
		chain:    chain,
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	g.handleHealth().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp HealthResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("status = %q, want %q", resp.Status, "ok")
	}
	if resp.Sessions != 1 {
		t.Errorf("sessions = %d, want 1", resp.Sessions)
	}
	if len(resp.Providers) != 1 {
		t.Errorf("providers = %d, want 1", len(resp.Providers))
	}
}

func TestHealth_Degraded(t *testing.T) {
	t.Parallel()

	chain := newTestChain(t, []provider.ChainEntry{
		{
			Name:     "p1",
			Provider: &fakeProvider{name: "p1", failErr: provider.ErrProviderDown},
			Role:     provider.RolePrimary,
			Health:   provider.HealthConfig{MaxFailures: 1, InitialBackoff: time.Hour},
		},
	})

	// Drive provider to dead.
	_, _ = chain.Complete(t.Context(), provider.RolePrimary, provider.CompletionRequest{})

	g := &Gateway{chain: chain}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	g.handleHealth().ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}

	var resp HealthResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Status != "degraded" {
		t.Errorf("status = %q, want %q", resp.Status, "degraded")
	}
}

func TestHealth_NoChain(t *testing.T) {
	t.Parallel()

	g := &Gateway{}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	g.handleHealth().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp HealthResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("status = %q, want %q", resp.Status, "ok")
	}
}
