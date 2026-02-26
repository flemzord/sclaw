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

func TestStatus_ReturnsMetrics(t *testing.T) {
	t.Parallel()

	store := router.NewInMemorySessionStore()
	store.GetOrCreate(router.SessionKey{Channel: "ch", ChatID: "1"})
	store.GetOrCreate(router.SessionKey{Channel: "ch", ChatID: "2"})

	chain := newTestChain(t, []provider.ChainEntry{
		{Name: "p1", Provider: &fakeProvider{name: "p1"}, Role: provider.RolePrimary},
	})

	m := &Metrics{}
	m.RecordCompletion(50, 100*time.Millisecond)
	m.RecordMessage()
	m.RecordError()

	g := &Gateway{
		sessions:  store,
		chain:     chain,
		metrics:   m,
		startedAt: time.Now().Add(-5 * time.Minute),
	}

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rr := httptest.NewRecorder()
	g.handleStatus().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp StatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Sessions != 2 {
		t.Errorf("sessions = %d, want 2", resp.Sessions)
	}
	if resp.Metrics.Completions != 1 {
		t.Errorf("completions = %d, want 1", resp.Metrics.Completions)
	}
	if resp.Metrics.Messages != 1 {
		t.Errorf("messages = %d, want 1", resp.Metrics.Messages)
	}
	if resp.Metrics.Errors != 1 {
		t.Errorf("errors = %d, want 1", resp.Metrics.Errors)
	}
	if resp.Uptime < 290 { // at least 290s (it's been 5 minutes)
		t.Errorf("uptime = %d, expected >= 290", resp.Uptime)
	}
}
