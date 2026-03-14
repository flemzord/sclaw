package tracing

import (
	"context"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/agent"
	"github.com/flemzord/sclaw/internal/hook"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/tool"
	"github.com/flemzord/sclaw/pkg/message"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

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

func newTestTracer(t *testing.T) (trace.Tracer, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return tp.Tracer("test"), exporter
}

func TestStartSpanHook_CreatesSpan(t *testing.T) {
	tracer, _ := newTestTracer(t)
	h := &startSpanHook{tracer: tracer}

	if h.Position() != hook.BeforeProcess {
		t.Fatalf("expected before_process, got %s", h.Position())
	}

	meta := make(map[string]any)
	hctx := &hook.Context{
		Position: hook.BeforeProcess,
		Inbound:  message.InboundMessage{Channel: "telegram"},
		Session:  &stubSession{id: "s1", channel: "telegram", chatID: "123"},
		Metadata: meta,
	}

	action, err := h.Execute(context.Background(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != hook.ActionContinue {
		t.Fatalf("expected ActionContinue, got %d", action)
	}

	span, ok := meta[metadataKeySpan].(trace.Span)
	if !ok || span == nil {
		t.Fatal("span not stored in metadata")
	}

	spanCtx, ok := meta[metadataKeyCtx].(context.Context)
	if !ok || spanCtx == nil {
		t.Fatal("span context not stored in metadata")
	}

	startTime, ok := meta[metadataKeyStartTime].(time.Time)
	if !ok {
		t.Fatal("start time not stored in metadata")
	}
	if time.Since(startTime) > time.Second {
		t.Fatal("start time is too old")
	}
}

func TestEndSpanHook_EndsSpanWithAttributes(t *testing.T) {
	tracer, exporter := newTestTracer(t)
	h := endSpanHook{}

	if h.Position() != hook.AfterSend {
		t.Fatalf("expected after_send, got %s", h.Position())
	}

	// Create a span to end.
	ctx, span := tracer.Start(context.Background(), "test")

	meta := map[string]any{
		metadataKeySpan:      span,
		metadataKeyCtx:       ctx,
		metadataKeyStartTime: time.Now().Add(-200 * time.Millisecond),
	}

	hctx := &hook.Context{
		Position: hook.AfterSend,
		Inbound:  message.InboundMessage{Channel: "telegram"},
		Outbound: &message.OutboundMessage{},
		Response: &agent.Response{
			Model:    "gpt-4o",
			Provider: "provider.openai_compatible",
			TotalUsage: provider.TokenUsage{
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
			},
			Iterations: 2,
			StopReason: "complete",
			ToolCalls: []agent.ToolCallRecord{
				{Name: "exec", Duration: 500 * time.Millisecond, Output: tool.Output{Content: "ok"}},
				{Name: "read_file", Duration: 100 * time.Millisecond, Output: tool.Output{IsError: true}},
			},
		},
		Session:  &stubSession{id: "s1", channel: "telegram", chatID: "123"},
		Metadata: meta,
	}

	action, err := h.Execute(context.Background(), hctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if action != hook.ActionContinue {
		t.Fatalf("expected ActionContinue, got %d", action)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	s := spans[0]

	// Check attributes.
	attrMap := make(map[string]attribute.Value)
	for _, a := range s.Attributes {
		attrMap[string(a.Key)] = a.Value
	}

	if v, ok := attrMap["sclaw.model"]; !ok || v.AsString() != "gpt-4o" {
		t.Error("missing or wrong sclaw.model attribute")
	}
	if v, ok := attrMap["sclaw.provider"]; !ok || v.AsString() != "provider.openai_compatible" {
		t.Error("missing or wrong sclaw.provider attribute")
	}
	if v, ok := attrMap["sclaw.tokens.input"]; !ok || v.AsInt64() != 100 {
		t.Error("missing or wrong sclaw.tokens.input attribute")
	}
	if v, ok := attrMap["sclaw.iterations"]; !ok || v.AsInt64() != 2 {
		t.Error("missing or wrong sclaw.iterations attribute")
	}
	if v, ok := attrMap["sclaw.tool_calls.count"]; !ok || v.AsInt64() != 2 {
		t.Error("missing or wrong sclaw.tool_calls.count attribute")
	}

	// Check span events (tool calls).
	if len(s.Events) != 2 {
		t.Fatalf("expected 2 span events, got %d", len(s.Events))
	}
	if s.Events[0].Name != "tool_call:exec" {
		t.Errorf("expected event name 'tool_call:exec', got %q", s.Events[0].Name)
	}
	if s.Events[1].Name != "tool_call:read_file" {
		t.Errorf("expected event name 'tool_call:read_file', got %q", s.Events[1].Name)
	}
}

func TestEndSpanHook_NilSpan(t *testing.T) {
	h := endSpanHook{}

	hctx := &hook.Context{
		Position: hook.AfterSend,
		Inbound:  message.InboundMessage{Channel: "test"},
		Response: &agent.Response{},
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

func TestEndSpanHook_ErrorStopReason(t *testing.T) {
	tracer, exporter := newTestTracer(t)
	_, span := tracer.Start(context.Background(), "test")

	meta := map[string]any{
		metadataKeySpan: span,
	}

	hctx := &hook.Context{
		Position: hook.AfterSend,
		Inbound:  message.InboundMessage{Channel: "test"},
		Response: &agent.Response{
			StopReason: "error",
			Model:      "test",
			Provider:   "test",
		},
		Session:  &stubSession{id: "s1", channel: "test", chatID: "1"},
		Metadata: meta,
	}

	var eh endSpanHook
	_, _ = eh.Execute(context.Background(), hctx)

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status.Code != codes.Error {
		t.Errorf("expected error status (%d), got %d", codes.Error, spans[0].Status.Code)
	}
}

func TestConfig_Defaults(t *testing.T) {
	c := Config{}
	c.defaults()

	if c.Exporter != "otlp" {
		t.Errorf("expected otlp, got %s", c.Exporter)
	}
	if c.OTLP.Endpoint != "localhost:4318" {
		t.Errorf("expected localhost:4318, got %s", c.OTLP.Endpoint)
	}
	if c.OTLP.Protocol != "http" {
		t.Errorf("expected http, got %s", c.OTLP.Protocol)
	}
	if c.Service.Name != "sclaw" {
		t.Errorf("expected sclaw, got %s", c.Service.Name)
	}
	if c.Sampling.Rate != 1.0 {
		t.Errorf("expected 1.0, got %f", c.Sampling.Rate)
	}
}
