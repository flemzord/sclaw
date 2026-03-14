package metrics

import "github.com/prometheus/client_golang/prometheus"

// collectors groups all Prometheus metric collectors for sclaw.
type collectors struct {
	messagesTotal  *prometheus.CounterVec
	tokensUsed     *prometheus.CounterVec
	latencySeconds *prometheus.HistogramVec
	providerErrors *prometheus.CounterVec
	toolCallsTotal *prometheus.CounterVec
	activeSessions prometheus.Gauge
	costDollars    *prometheus.CounterVec
}

func newCollectors(reg prometheus.Registerer) *collectors {
	c := &collectors{
		messagesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "sclaw_messages_total",
			Help: "Total number of messages processed.",
		}, []string{"channel", "direction"}),

		tokensUsed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "sclaw_tokens_used",
			Help: "Total tokens consumed.",
		}, []string{"type", "provider", "model"}),

		latencySeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "sclaw_response_latency_seconds",
			Help:    "End-to-end response latency.",
			Buckets: prometheus.DefBuckets,
		}, []string{}),

		providerErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "sclaw_provider_errors_total",
			Help: "Total provider errors.",
		}, []string{"error_type", "model"}),

		toolCallsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "sclaw_tool_calls_total",
			Help: "Total tool calls executed.",
		}, []string{"tool_name", "result"}),

		activeSessions: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "sclaw_active_sessions",
			Help: "Number of active sessions.",
		}),

		costDollars: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "sclaw_cost_dollars",
			Help: "Estimated cost in US dollars.",
		}, []string{"provider", "model"}),
	}

	reg.MustRegister(
		c.messagesTotal,
		c.tokensUsed,
		c.latencySeconds,
		c.providerErrors,
		c.toolCallsTotal,
		c.activeSessions,
		c.costDollars,
	)

	return c
}
