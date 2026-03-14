package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/hook"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/tool"
	"github.com/flemzord/sclaw/pkg/message"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// stubSession implements hook.SessionView for testing.
type stubSession struct {
	id      string
	channel string
	chatID  string
}

func (s *stubSession) SessionID() string                    { return s.id }
func (s *stubSession) SessionKey() (string, string, string) { return s.channel, s.chatID, "" }
func (s *stubSession) AgentID() string                      { return "test-agent" }
func (s *stubSession) CreatedAt() time.Time                 { return time.Now() }
func (s *stubSession) GetMetadata(_ string) (any, bool)     { return nil, false }

func newTestCollectors(t *testing.T) (*collectors, *prometheus.Registry) {
	t.Helper()
	reg := prometheus.NewRegistry()
	c := newCollectors(reg)
	return c, reg
}

func scrapeMetrics(t *testing.T, reg *prometheus.Registry) string {
	t.Helper()
	handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil))
	return rec.Body.String()
}

func TestTimingHook_StampsStartTime(t *testing.T) {
	h := timingHook{}

	if h.Position() != hook.BeforeProcess {
		t.Fatalf("expected before_process, got %s", h.Position())
	}

	meta := make(map[string]any)
	hctx := &hook.Context{
		Position: hook.BeforeProcess,
		Metadata: meta,
	}

	action, err := h.Execute(context.Background(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != hook.ActionContinue {
		t.Fatalf("expected ActionContinue, got %d", action)
	}

	startTime, ok := meta[metadataKeyStartTime].(time.Time)
	if !ok {
		t.Fatal("start_time not stamped in metadata")
	}
	if time.Since(startTime) > time.Second {
		t.Fatal("start_time is too old")
	}
}

func TestMetricsHook_MessageCounters(t *testing.T) {
	c, reg := newTestCollectors(t)
	h := &metricsHook{collectors: c, config: &Config{}}

	if h.Position() != hook.AfterSend {
		t.Fatalf("expected after_send, got %s", h.Position())
	}

	hctx := &hook.Context{
		Position: hook.AfterSend,
		Inbound: message.InboundMessage{
			Channel: "channel.telegram",
		},
		Outbound: &message.OutboundMessage{},
		Response: &agent.Response{
			Model:    "gpt-4o",
			Provider: "provider.openai_compatible",
		},
		Session:  &stubSession{id: "s1", channel: "channel.telegram", chatID: "123"},
		Metadata: make(map[string]any),
	}

	action, err := h.Execute(context.Background(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != hook.ActionContinue {
		t.Fatalf("expected ActionContinue, got %d", action)
	}

	body := scrapeMetrics(t, reg)
	if !strings.Contains(body, `sclaw_messages_total{channel="channel.telegram",direction="inbound"}`) {
		t.Error("missing inbound message counter")
	}
	if !strings.Contains(body, `sclaw_messages_total{channel="channel.telegram",direction="outbound"}`) {
		t.Error("missing outbound message counter")
	}
}

func TestMetricsHook_TokenUsage(t *testing.T) {
	c, reg := newTestCollectors(t)
	h := &metricsHook{collectors: c, config: &Config{}}

	hctx := &hook.Context{
		Position: hook.AfterSend,
		Inbound:  message.InboundMessage{Channel: "test"},
		Response: &agent.Response{
			Model:    "gpt-4o",
			Provider: "openai",
			TotalUsage: provider.TokenUsage{
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
			},
		},
		Session:  &stubSession{id: "s1", channel: "test", chatID: "1"},
		Metadata: make(map[string]any),
	}

	_, _ = h.Execute(context.Background(), hctx)

	body := scrapeMetrics(t, reg)
	if !strings.Contains(body, `sclaw_tokens_used{model="gpt-4o",provider="openai",type="input"} 100`) {
		t.Error("missing input token counter")
	}
	if !strings.Contains(body, `sclaw_tokens_used{model="gpt-4o",provider="openai",type="output"} 50`) {
		t.Error("missing output token counter")
	}
}

func TestMetricsHook_Latency(t *testing.T) {
	c, reg := newTestCollectors(t)
	h := &metricsHook{collectors: c, config: &Config{}}

	meta := map[string]any{
		metadataKeyStartTime: time.Now().Add(-100 * time.Millisecond),
	}

	hctx := &hook.Context{
		Position: hook.AfterSend,
		Inbound:  message.InboundMessage{Channel: "test"},
		Response: &agent.Response{Model: "test", Provider: "test"},
		Session:  &stubSession{id: "s1", channel: "test", chatID: "1"},
		Metadata: meta,
	}

	_, _ = h.Execute(context.Background(), hctx)

	body := scrapeMetrics(t, reg)
	if !strings.Contains(body, "sclaw_response_latency_seconds") {
		t.Error("missing latency histogram")
	}
}

func TestMetricsHook_ToolCalls(t *testing.T) {
	c, reg := newTestCollectors(t)
	h := &metricsHook{collectors: c, config: &Config{}}

	hctx := &hook.Context{
		Position: hook.AfterSend,
		Inbound:  message.InboundMessage{Channel: "test"},
		Response: &agent.Response{
			Model:    "test",
			Provider: "test",
			ToolCalls: []agent.ToolCallRecord{
				{Name: "exec", Output: tool.Output{Content: "ok"}},
				{Name: "read_file", Output: tool.Output{Content: "", IsError: true}},
				{Name: "write_file", Panicked: true, Output: tool.Output{}},
			},
		},
		Session:  &stubSession{id: "s1", channel: "test", chatID: "1"},
		Metadata: make(map[string]any),
	}

	_, _ = h.Execute(context.Background(), hctx)

	body := scrapeMetrics(t, reg)
	if !strings.Contains(body, `sclaw_tool_calls_total{result="success",tool_name="exec"} 1`) {
		t.Error("missing successful tool call counter")
	}
	if !strings.Contains(body, `sclaw_tool_calls_total{result="error",tool_name="read_file"} 1`) {
		t.Error("missing errored tool call counter")
	}
	if !strings.Contains(body, `sclaw_tool_calls_total{result="error",tool_name="write_file"} 1`) {
		t.Error("missing panicked tool call counter")
	}
}

func TestMetricsHook_CostTracking(t *testing.T) {
	c, reg := newTestCollectors(t)
	cfg := &Config{
		Cost: CostConfig{
			Enabled: true,
			Prices: map[string]ModelPricing{
				"gpt-4o": {Input: 2.5, Output: 10.0},
			},
		},
	}
	h := &metricsHook{collectors: c, config: cfg}

	hctx := &hook.Context{
		Position: hook.AfterSend,
		Inbound:  message.InboundMessage{Channel: "test"},
		Response: &agent.Response{
			Model:    "gpt-4o",
			Provider: "openai",
			TotalUsage: provider.TokenUsage{
				PromptTokens:     1_000_000,
				CompletionTokens: 500_000,
			},
		},
		Session:  &stubSession{id: "s1", channel: "test", chatID: "1"},
		Metadata: make(map[string]any),
	}

	_, _ = h.Execute(context.Background(), hctx)

	body := scrapeMetrics(t, reg)
	// 1M input tokens * $2.5/M = $2.5, 500K output tokens * $10/M = $5.0 → total $7.5
	if !strings.Contains(body, `sclaw_cost_dollars{model="gpt-4o",provider="openai"} 7.5`) {
		t.Errorf("unexpected cost metric, got:\n%s", body)
	}
}

func TestMetricsHook_NilResponse(t *testing.T) {
	c, _ := newTestCollectors(t)
	h := &metricsHook{collectors: c, config: &Config{}}

	hctx := &hook.Context{
		Position: hook.AfterSend,
		Inbound:  message.InboundMessage{Channel: "test"},
		Response: nil,
		Session:  &stubSession{id: "s1", channel: "test", chatID: "1"},
		Metadata: make(map[string]any),
	}

	action, err := h.Execute(context.Background(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != hook.ActionContinue {
		t.Fatalf("expected ActionContinue, got %d", action)
	}
}

func TestMetricsEndpoint_Integration(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := newCollectors(reg)

	// Simulate some metrics.
	c.messagesTotal.WithLabelValues("telegram", "inbound").Inc()
	c.activeSessions.Set(5)

	handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "sclaw_messages_total") {
		t.Error("missing sclaw_messages_total in output")
	}
	if !strings.Contains(body, "sclaw_active_sessions 5") {
		t.Error("missing sclaw_active_sessions in output")
	}
}

func TestConfig_Defaults(t *testing.T) {
	c := Config{}
	c.defaults()

	if c.Exporter != "prometheus" {
		t.Errorf("expected prometheus, got %s", c.Exporter)
	}
	if c.Prometheus.Path != "/metrics" {
		t.Errorf("expected /metrics, got %s", c.Prometheus.Path)
	}
}
