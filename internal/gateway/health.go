package gateway

import (
	"encoding/json"
	"net/http"

	"github.com/flemzord/sclaw/internal/provider"
)

// HealthResponse is the JSON response for GET /health.
type HealthResponse struct {
	Status    string            `json:"status"` // "ok" or "degraded"
	Sessions  int               `json:"sessions"`
	Providers []provider.Status `json:"providers"`
}

// handleHealth returns an http.HandlerFunc for GET /health.
// Returns 200 if all providers are healthy, 503 if any is degraded/dead.
func (g *Gateway) handleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		resp := HealthResponse{
			Status: "ok",
		}

		if g.sessions != nil {
			resp.Sessions = g.sessions.Len()
		}

		if g.chain != nil {
			resp.Providers = g.chain.HealthReport()
			for _, p := range resp.Providers {
				if !p.Available {
					resp.Status = "degraded"
					break
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if resp.Status == "degraded" {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}
