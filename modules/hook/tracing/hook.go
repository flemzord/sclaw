package tracing

import (
	"context"
	"fmt"
	"time"

	"github.com/flemzord/sclaw/internal/hook"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	metadataKeySpan      = "tracing_span"
	metadataKeyCtx       = "tracing_ctx"
	metadataKeyStartTime = "tracing_start_time"
)

// startSpanHook creates a root span at before_process and stores it in metadata.
type startSpanHook struct {
	tracer trace.Tracer
}

func (h *startSpanHook) Position() hook.Position { return hook.BeforeProcess }
func (h *startSpanHook) Priority() int           { return 0 }

func (h *startSpanHook) Execute(ctx context.Context, hctx *hook.Context) (hook.Action, error) {
	channel, chatID, _ := hctx.Session.SessionKey()

	spanCtx, span := h.tracer.Start(ctx, "sclaw.message",
		trace.WithAttributes(
			attribute.String("sclaw.session.id", hctx.Session.SessionID()),
			attribute.String("sclaw.session.agent", hctx.Session.AgentID()),
			attribute.String("sclaw.channel", channel),
			attribute.String("sclaw.chat_id", chatID),
			attribute.String("sclaw.message.direction", "inbound"),
		),
		trace.WithSpanKind(trace.SpanKindServer),
	)

	hctx.Metadata[metadataKeySpan] = span
	hctx.Metadata[metadataKeyCtx] = spanCtx
	hctx.Metadata[metadataKeyStartTime] = time.Now()

	return hook.ActionContinue, nil
}

// endSpanHook ends the root span at after_send with response attributes.
type endSpanHook struct{}

func (endSpanHook) Position() hook.Position { return hook.AfterSend }
func (endSpanHook) Priority() int           { return 200 }

func (endSpanHook) Execute(_ context.Context, hctx *hook.Context) (hook.Action, error) {
	span, ok := hctx.Metadata[metadataKeySpan].(trace.Span)
	if !ok || span == nil {
		return hook.ActionContinue, nil
	}
	defer span.End()

	resp := hctx.Response
	if resp == nil {
		span.SetStatus(codes.Error, "no response")
		return hook.ActionContinue, nil
	}

	// Response attributes.
	span.SetAttributes(
		attribute.String("sclaw.model", resp.Model),
		attribute.String("sclaw.provider", resp.Provider),
		attribute.Int("sclaw.tokens.input", resp.TotalUsage.PromptTokens),
		attribute.Int("sclaw.tokens.output", resp.TotalUsage.CompletionTokens),
		attribute.Int("sclaw.tokens.total", resp.TotalUsage.TotalTokens),
		attribute.Int("sclaw.iterations", resp.Iterations),
		attribute.String("sclaw.stop_reason", string(resp.StopReason)),
		attribute.Int("sclaw.tool_calls.count", len(resp.ToolCalls)),
	)

	// Latency.
	if startTime, ok := hctx.Metadata[metadataKeyStartTime].(time.Time); ok {
		span.SetAttributes(attribute.Float64("sclaw.latency_seconds", time.Since(startTime).Seconds()))
	}

	// Tool calls as span events.
	for _, tc := range resp.ToolCalls {
		attrs := []attribute.KeyValue{
			attribute.String("tool.name", tc.Name),
			attribute.String("tool.duration", tc.Duration.String()),
		}
		if tc.Output.IsError {
			attrs = append(attrs, attribute.Bool("tool.error", true))
		}
		if tc.Panicked {
			attrs = append(attrs, attribute.Bool("tool.panicked", true))
		}
		span.AddEvent(fmt.Sprintf("tool_call:%s", tc.Name), trace.WithAttributes(attrs...))
	}

	// Set span status based on stop reason.
	switch resp.StopReason {
	case "complete":
		span.SetStatus(codes.Ok, "complete")
	case "error":
		span.SetStatus(codes.Error, "agent loop error")
	default:
		span.SetStatus(codes.Ok, string(resp.StopReason))
	}

	return hook.ActionContinue, nil
}
