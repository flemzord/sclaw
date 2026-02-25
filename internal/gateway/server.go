package gateway

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// buildRouter constructs the chi mux with all routes wired.
func (g *Gateway) buildRouter() http.Handler {
	r := chi.NewRouter()

	// Public — no auth required.
	r.Get("/health", g.handleHealth())

	// Webhooks — own HMAC auth per source.
	r.Post("/webhooks/{source}", g.dispatcher.ServeHTTP)

	// Node WebSocket — device connections (pairing token auth, not bearer).
	if svc, ok := g.appCtx.Service("node.handler"); ok {
		if handler, ok := svc.(http.Handler); ok {
			r.Handle("/ws/node", handler)
		}
	}

	// Admin endpoints — auth required. Not mounted if no auth configured.
	if g.config.Auth.IsConfigured() {
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware(g.config.Auth))
			r.Get("/status", g.handleStatus())
			r.Route("/api", func(r chi.Router) {
				r.Get("/sessions", g.handleListSessions())
				r.Delete("/sessions/{id}", g.handleDeleteSession())
				r.Get("/agents", g.handleListAgents())
				r.Get("/modules", g.handleGetAllModules())
				r.Get("/config", g.handleGetConfig())
				r.Post("/config/reload", g.handleReloadConfig())
			})
		})
	}

	return r
}
