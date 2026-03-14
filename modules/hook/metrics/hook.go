package metrics

import (
	"context"
	"time"

	"github.com/flemzord/sclaw/internal/hook"
)

const metadataKeyStartTime = "metrics_start_time"

// timingHook stamps the start time into hook metadata at before_process.
type timingHook struct{}

func (timingHook) Position() hook.Position { return hook.BeforeProcess }
func (timingHook) Priority() int           { return 0 }

func (timingHook) Execute(_ context.Context, hctx *hook.Context) (hook.Action, error) {
	hctx.Metadata[metadataKeyStartTime] = time.Now()
	return hook.ActionContinue, nil
}

// metricsHook collects all observability metrics at after_send.
type metricsHook struct {
	collectors *collectors
	config     *Config
}

func (h *metricsHook) Position() hook.Position { return hook.AfterSend }
func (h *metricsHook) Priority() int           { return 100 }

func (h *metricsHook) Execute(_ context.Context, hctx *hook.Context) (hook.Action, error) {
	c := h.collectors

	// Message counters.
	channel, _, _ := hctx.Session.SessionKey()
	c.messagesTotal.WithLabelValues(channel, "inbound").Inc()
	if hctx.Outbound != nil {
		c.messagesTotal.WithLabelValues(channel, "outbound").Inc()
	}

	resp := hctx.Response
	if resp == nil {
		return hook.ActionContinue, nil
	}

	model := resp.Model
	prov := resp.Provider

	// Token usage.
	usage := resp.TotalUsage
	if usage.PromptTokens > 0 {
		c.tokensUsed.WithLabelValues("input", prov, model).Add(float64(usage.PromptTokens))
	}
	if usage.CompletionTokens > 0 {
		c.tokensUsed.WithLabelValues("output", prov, model).Add(float64(usage.CompletionTokens))
	}

	// Latency.
	if startTime, ok := hctx.Metadata[metadataKeyStartTime].(time.Time); ok {
		c.latencySeconds.WithLabelValues().Observe(time.Since(startTime).Seconds())
	}

	// Provider errors (indicated by error stop reason).
	if resp.StopReason == "error" {
		c.providerErrors.WithLabelValues("loop_error", model).Inc()
	}

	// Tool calls.
	for _, tc := range resp.ToolCalls {
		result := "success"
		if tc.Output.IsError || tc.Panicked {
			result = "error"
		}
		c.toolCallsTotal.WithLabelValues(tc.Name, result).Inc()
	}

	// Cost tracking.
	if h.config.Cost.Enabled {
		if pricing, ok := h.config.Cost.Prices[model]; ok {
			inputCost := float64(usage.PromptTokens) / 1_000_000 * pricing.Input
			outputCost := float64(usage.CompletionTokens) / 1_000_000 * pricing.Output
			if total := inputCost + outputCost; total > 0 {
				c.costDollars.WithLabelValues(prov, model).Add(total)
			}
		}
	}

	return hook.ActionContinue, nil
}
