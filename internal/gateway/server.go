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

	// Metrics endpoint — mounted if hook.metrics module is loaded.
	if handler, path := g.resolveMetricsHandler(); handler != nil {
		r.Get(path, handler.ServeHTTP)
	}

	// Webhooks — own HMAC auth per source.
	r.Post("/webhooks/{source}", g.dispatcher.ServeHTTP)

	// Admin endpoints — auth required. Not mounted if no auth configured.
	if g.config.Auth.IsConfigured() {
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware(g.config.Auth, g.auditLogger, g.rateLimiter))
			r.Get("/status", g.handleStatus())
			r.Route("/api", func(r chi.Router) {
				r.Get("/sessions", g.handleListSessions())
				r.Delete("/sessions/{id}", g.handleDeleteSession())
				r.Get("/agents", g.handleListAgents())
				r.Get("/modules", g.handleGetAllModules())
				r.Get("/config", g.handleGetConfig())
				r.Post("/config/reload", g.handleReloadConfig())
				r.Get("/crons", g.handleListCrons())
				r.Get("/crons/{name}", g.handleGetCron())
				r.Post("/crons/{name}/trigger", g.handleTriggerCron())
				r.Get("/openapi.yaml", g.handleOpenAPI())
			})
		})
	} else {
		g.logger.Warn("gateway: admin API disabled (no auth configured)")
	}

	return r
}
