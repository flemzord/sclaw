package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flemzord/sclaw/internal/router"
	"github.com/flemzord/sclaw/internal/security"
	"github.com/go-chi/chi/v5"
)

func TestAdmin_ListSessions_Empty(t *testing.T) {
	t.Parallel()

	g := &Gateway{
		sessions: router.NewInMemorySessionStore(),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rr := httptest.NewRecorder()
	g.handleListSessions().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var sessions []sessionJSON
	if err := json.NewDecoder(rr.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("sessions = %d, want 0", len(sessions))
	}
}

func TestAdmin_ListSessions_WithData(t *testing.T) {
	t.Parallel()

	store := router.NewInMemorySessionStore()
	store.GetOrCreate(router.SessionKey{Channel: "test", ChatID: "chat1"})
	store.GetOrCreate(router.SessionKey{Channel: "test", ChatID: "chat2", ThreadID: "t1"})

	g := &Gateway{sessions: store}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rr := httptest.NewRecorder()
	g.handleListSessions().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var sessions []sessionJSON
	if err := json.NewDecoder(rr.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("sessions = %d, want 2", len(sessions))
	}
}

func TestAdmin_ListSessions_NilStore(t *testing.T) {
	t.Parallel()

	g := &Gateway{}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rr := httptest.NewRecorder()
	g.handleListSessions().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var sessions []sessionJSON
	if err := json.NewDecoder(rr.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("sessions = %d, want 0", len(sessions))
	}
}

func TestAdmin_DeleteSession_Found(t *testing.T) {
	t.Parallel()

	store := router.NewInMemorySessionStore()
	sess, _ := store.GetOrCreate(router.SessionKey{Channel: "ch", ChatID: "c1"})
	id := sess.ID

	g := &Gateway{sessions: store}

	// We need chi to extract the URL param.
	r := chi.NewRouter()
	r.Delete("/api/sessions/{id}", g.handleDeleteSession())

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+id, nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusNoContent)
	}

	if store.Len() != 0 {
		t.Errorf("store.Len() = %d, want 0", store.Len())
	}
}

func TestAdmin_DeleteSession_NotFound(t *testing.T) {
	t.Parallel()

	g := &Gateway{sessions: router.NewInMemorySessionStore()}

	r := chi.NewRouter()
	r.Delete("/api/sessions/{id}", g.handleDeleteSession())

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/nonexistent", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestAdmin_RedactSecrets(t *testing.T) {
	t.Parallel()

	m := map[string]any{
		"name":         "test",
		"api_key":      "should-be-redacted",
		"bearer_token": "also-secret",
		"password":     "hide-me",
		"nested": map[string]any{
			"secret": "inner-secret",
			"normal": "visible",
		},
		"list": []any{
			map[string]any{
				"token": "list-secret",
			},
		},
	}

	r := security.NewRedactor()
	r.RedactMap(m)

	if m["api_key"] != security.RedactPlaceholder {
		t.Errorf("api_key = %q, want redacted", m["api_key"])
	}
	if m["bearer_token"] != security.RedactPlaceholder {
		t.Errorf("bearer_token = %q, want redacted", m["bearer_token"])
	}
	if m["password"] != security.RedactPlaceholder {
		t.Errorf("password = %q, want redacted", m["password"])
	}
	if m["name"] != "test" {
		t.Errorf("name = %q, want %q", m["name"], "test")
	}

	nested := m["nested"].(map[string]any)
	if nested["secret"] != security.RedactPlaceholder {
		t.Errorf("nested.secret = %q, want redacted", nested["secret"])
	}
	if nested["normal"] != "visible" {
		t.Errorf("nested.normal = %q, want %q", nested["normal"], "visible")
	}

	list := m["list"].([]any)
	item := list[0].(map[string]any)
	if item["token"] != security.RedactPlaceholder {
		t.Errorf("list[0].token = %q, want redacted", item["token"])
	}
}

func TestAdmin_RedactSecrets_EmptyValue(t *testing.T) {
	t.Parallel()

	m := map[string]any{
		"api_key": "",
		"secret":  "",
	}

	r := security.NewRedactor()
	r.RedactMap(m)

	// Empty string values should NOT be redacted (nothing to hide).
	if m["api_key"] != "" {
		t.Errorf("empty api_key should not be redacted, got %q", m["api_key"])
	}
}
