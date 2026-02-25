package gateway

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/flemzord/sclaw/internal/provider"
)

// StatusResponse is the JSON response for GET /status.
type StatusResponse struct {
	Uptime    time.Duration     `json:"uptime_seconds"`
	Metrics   MetricsSnapshot   `json:"metrics"`
	Sessions  int               `json:"sessions"`
	Providers []provider.Status `json:"providers"`
}

// handleStatus returns an http.HandlerFunc for GET /status.
func (g *Gateway) handleStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		resp := StatusResponse{
			Uptime:  time.Since(g.startedAt).Truncate(time.Second),
			Metrics: g.metrics.Snapshot(),
		}

		if g.sessions != nil {
			resp.Sessions = g.sessions.Len()
		}

		if g.chain != nil {
			resp.Providers = g.chain.HealthReport()
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}
