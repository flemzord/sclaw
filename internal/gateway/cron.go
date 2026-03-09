package gateway

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/flemzord/sclaw/internal/cron"
	"github.com/go-chi/chi/v5"
)

// handleListCrons returns all registered prompt crons as JSON.
func (g *Gateway) handleListCrons() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if g.cronTrigger == nil {
			writeJSON(w, http.StatusOK, []cron.Info{})
			return
		}

		infos := g.cronTrigger.List()
		if infos == nil {
			infos = []cron.Info{}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(infos)
	}
}

// handleGetCron returns a single prompt cron definition with its last result.
func (g *Gateway) handleGetCron() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		if name == "" {
			http.Error(w, "missing cron name", http.StatusBadRequest)
			return
		}

		if g.cronTrigger == nil {
			http.Error(w, "cron trigger not available", http.StatusServiceUnavailable)
			return
		}

		info, ok := g.cronTrigger.Get(name)
		if !ok {
			http.Error(w, "cron not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(info)
	}
}

// triggerResponse is the JSON response for POST /api/crons/{name}/trigger.
type triggerResponse struct {
	Status  string `json:"status"`
	Name    string `json:"name"`
	AgentID string `json:"agent_id"`
}

// handleTriggerCron fires a prompt cron manually in the background.
func (g *Gateway) handleTriggerCron() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		if name == "" {
			http.Error(w, "missing cron name", http.StatusBadRequest)
			return
		}

		if g.cronTrigger == nil {
			http.Error(w, "cron trigger not available", http.StatusServiceUnavailable)
			return
		}

		info, ok := g.cronTrigger.Get(name)
		if !ok {
			http.Error(w, "cron not found", http.StatusNotFound)
			return
		}

		// Run the cron job in the background. Use a detached context because
		// the HTTP request context is cancelled when the handler returns.
		go func() {
			defer func() {
				if r := recover(); r != nil {
					g.logger.Error("cron trigger panicked", "name", name, "panic", r)
				}
			}()
			if err := g.cronTrigger.Trigger(context.Background(), name); err != nil {
				g.logger.Error("cron trigger failed", "name", name, "error", err)
			}
		}()

		writeJSON(w, http.StatusAccepted, triggerResponse{
			Status:  "triggered",
			Name:    name,
			AgentID: info.AgentID,
		})
	}
}
