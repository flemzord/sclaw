// Package gateway provides an HTTP server for administration, monitoring,
// and webhooks. It binds to loopback by default and follows the module system pattern.
package gateway

import (
	"encoding/json"
	"net/http"

	"github.com/flemzord/sclaw/internal/config"
	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/router"
	"github.com/flemzord/sclaw/internal/security"
	"github.com/go-chi/chi/v5"
)

// sessionJSON is a serializable session snapshot.
type sessionJSON struct {
	ID           string         `json:"id"`
	Channel      string         `json:"channel"`
	ChatID       string         `json:"chat_id"`
	ThreadID     string         `json:"thread_id"`
	AgentID      string         `json:"agent_id"`
	CreatedAt    string         `json:"created_at"`
	LastActiveAt string         `json:"last_active_at"`
	HistoryLen   int            `json:"history_len"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// handleListSessions returns all active sessions as JSON.
func (g *Gateway) handleListSessions() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		var sessions []sessionJSON

		if g.sessions != nil {
			g.sessions.Range(func(key router.SessionKey, sess *router.Session) bool {
				// Shallow-copy metadata to avoid a data race if the pipeline
				// mutates the original map after we release the store lock.
				var metaCopy map[string]any
				if sess.Metadata != nil {
					metaCopy = make(map[string]any, len(sess.Metadata))
					for k, v := range sess.Metadata {
						metaCopy[k] = v
					}
				}
				sessions = append(sessions, sessionJSON{
					ID:           sess.ID,
					Channel:      key.Channel,
					ChatID:       key.ChatID,
					ThreadID:     key.ThreadID,
					AgentID:      sess.AgentID,
					CreatedAt:    sess.CreatedAt.Format("2006-01-02T15:04:05Z"),
					LastActiveAt: sess.LastActiveAt.Format("2006-01-02T15:04:05Z"),
					HistoryLen:   len(sess.History),
					Metadata:     metaCopy,
				})
				return true
			})
		}

		// Force empty JSON array instead of null for API consumers.
		if sessions == nil {
			sessions = []sessionJSON{}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sessions)
	}
}

// handleDeleteSession deletes a session by its ID.
func (g *Gateway) handleDeleteSession() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			http.Error(w, "missing session id", http.StatusBadRequest)
			return
		}

		if g.sessions == nil {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}

		// Find session key by ID (linear scan — sessions are typically few).
		// We must find the key first, then delete outside Range to avoid
		// deadlocking on RLock→Lock.
		var targetKey router.SessionKey
		var found bool

		g.sessions.Range(func(key router.SessionKey, sess *router.Session) bool {
			if sess.ID == id {
				targetKey = key
				found = true
				return false
			}
			return true
		})

		if found {
			g.sessions.Delete(targetKey)
		}

		if !found {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// agentJSON is a serializable module info snapshot.
type agentJSON struct {
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// handleListAgents lists agent modules (namespace "agent").
func (g *Gateway) handleListAgents() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		mods := core.GetModulesByNamespace("agent")
		agents := make([]agentJSON, 0, len(mods))
		for _, m := range mods {
			agents = append(agents, agentJSON{
				ID:        string(m.ID),
				Namespace: m.ID.Namespace(),
				Name:      m.ID.Name(),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(agents)
	}
}

// handleGetConfig returns the current config with secrets redacted.
func (g *Gateway) handleGetConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if g.appCtx == nil {
			http.Error(w, "config not available", http.StatusServiceUnavailable)
			return
		}

		cfgPath := g.configPath
		if cfgPath == "" {
			http.Error(w, "config path not set", http.StatusServiceUnavailable)
			return
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			http.Error(w, "failed to load config", http.StatusInternalServerError)
			return
		}

		raw, err := json.Marshal(cfg)
		if err != nil {
			http.Error(w, "failed to serialize config", http.StatusInternalServerError)
			return
		}

		var generic map[string]any
		if err := json.Unmarshal(raw, &generic); err != nil {
			http.Error(w, "failed to parse config", http.StatusInternalServerError)
			return
		}

		// Delegate to centralized Redactor if available, fall back to local redaction.
		if g.redactor != nil {
			g.redactor.RedactMap(generic)
		} else {
			security.NewRedactor().RedactMap(generic)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(generic)
	}
}

// handleReloadConfig triggers a hot-reload of the configuration.
func (g *Gateway) handleReloadConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfgPath := g.configPath
		if cfgPath == "" {
			http.Error(w, "config path not set", http.StatusServiceUnavailable)
			return
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			g.logger.Error("config reload failed", "error", err)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		if err := config.Validate(cfg); err != nil {
			g.logger.Error("config validation failed on reload", "error", err)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		if g.reloadHandler != nil {
			if err := g.reloadHandler.HandleReloadFromConfig(r.Context(), cfg); err != nil {
				g.logger.Error("config reload failed", "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "reload failed: " + err.Error()})
				return
			}
		} else {
			g.logger.Warn("reload handler not available, config validated but not applied")
			writeJSON(w, http.StatusOK, map[string]string{"status": "validated", "warning": "reload handler not available"})
			return
		}

		g.logger.Info("configuration reloaded successfully")
		if g.auditLogger != nil {
			g.auditLogger.Log(security.AuditEvent{
				Type:   security.EventConfigChange,
				Detail: "configuration reloaded via admin API",
			})
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
	}
}

// writeJSON encodes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// handleGetAllModules lists all compiled modules (for /api/modules).
func (g *Gateway) handleGetAllModules() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		mods := core.GetModules()
		out := make([]agentJSON, 0, len(mods))
		for _, m := range mods {
			out = append(out, agentJSON{
				ID:        string(m.ID),
				Namespace: m.ID.Namespace(),
				Name:      m.ID.Name(),
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}
