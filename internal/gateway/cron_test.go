package gateway

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flemzord/sclaw/internal/cron"
	"github.com/go-chi/chi/v5"
)

func TestCron_ListCrons_NilTrigger(t *testing.T) {
	t.Parallel()

	g := &Gateway{}
	req := httptest.NewRequest(http.MethodGet, "/api/crons", nil)
	rr := httptest.NewRecorder()
	g.handleListCrons().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var infos []cron.Info
	if err := json.NewDecoder(rr.Body).Decode(&infos); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("len = %d, want 0", len(infos))
	}
}

func TestCron_ListCrons_WithData(t *testing.T) {
	t.Parallel()

	ct := cron.NewTrigger()
	ct.Register(&cron.PromptJob{
		Def:     cron.PromptCronDef{Name: "test-cron", Schedule: "0 7 * * *", Enabled: true},
		AgentID: "main",
		DataDir: t.TempDir(),
	})

	g := &Gateway{cronTrigger: ct}
	req := httptest.NewRequest(http.MethodGet, "/api/crons", nil)
	rr := httptest.NewRecorder()
	g.handleListCrons().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var infos []cron.Info
	if err := json.NewDecoder(rr.Body).Decode(&infos); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("len = %d, want 1", len(infos))
	}
	if infos[0].Name != "test-cron" {
		t.Errorf("name = %q, want %q", infos[0].Name, "test-cron")
	}
}

func TestCron_GetCron_Found(t *testing.T) {
	t.Parallel()

	ct := cron.NewTrigger()
	ct.Register(&cron.PromptJob{
		Def:     cron.PromptCronDef{Name: "my-cron", Schedule: "30 8 * * *", Enabled: true, Description: "desc"},
		AgentID: "agent1",
		DataDir: t.TempDir(),
	})

	g := &Gateway{cronTrigger: ct}

	r := chi.NewRouter()
	r.Get("/api/crons/{name}", g.handleGetCron())

	req := httptest.NewRequest(http.MethodGet, "/api/crons/my-cron", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var info cron.Info
	if err := json.NewDecoder(rr.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.Name != "my-cron" {
		t.Errorf("name = %q, want %q", info.Name, "my-cron")
	}
	if info.AgentID != "agent1" {
		t.Errorf("agent_id = %q, want %q", info.AgentID, "agent1")
	}
}

func TestCron_GetCron_NotFound(t *testing.T) {
	t.Parallel()

	ct := cron.NewTrigger()
	g := &Gateway{cronTrigger: ct}

	r := chi.NewRouter()
	r.Get("/api/crons/{name}", g.handleGetCron())

	req := httptest.NewRequest(http.MethodGet, "/api/crons/nonexistent", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestCron_TriggerCron_NotFound(t *testing.T) {
	t.Parallel()

	ct := cron.NewTrigger()
	g := &Gateway{
		cronTrigger: ct,
		logger:      slog.Default(),
	}

	r := chi.NewRouter()
	r.Post("/api/crons/{name}/trigger", g.handleTriggerCron())

	req := httptest.NewRequest(http.MethodPost, "/api/crons/nonexistent/trigger", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestCron_TriggerCron_Accepted(t *testing.T) {
	t.Parallel()

	ct := cron.NewTrigger()
	ct.Register(&cron.PromptJob{
		Def:     cron.PromptCronDef{Name: "fire-me", Schedule: "0 0 * * *", Enabled: true},
		AgentID: "main",
		DataDir: t.TempDir(),
		// Builder is nil — the goroutine will fail, but the HTTP response is 202.
	})

	g := &Gateway{
		cronTrigger: ct,
		logger:      slog.Default(),
	}

	r := chi.NewRouter()
	r.Post("/api/crons/{name}/trigger", g.handleTriggerCron())

	req := httptest.NewRequest(http.MethodPost, "/api/crons/fire-me/trigger", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusAccepted)
	}

	var resp triggerResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "triggered" {
		t.Errorf("status = %q, want %q", resp.Status, "triggered")
	}
	if resp.Name != "fire-me" {
		t.Errorf("name = %q, want %q", resp.Name, "fire-me")
	}
}

func TestCron_TriggerCron_NilTrigger(t *testing.T) {
	t.Parallel()

	g := &Gateway{logger: slog.Default()}

	r := chi.NewRouter()
	r.Post("/api/crons/{name}/trigger", g.handleTriggerCron())

	req := httptest.NewRequest(http.MethodPost, "/api/crons/anything/trigger", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
}
