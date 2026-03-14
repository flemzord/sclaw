// Package tracing implements the hook.tracing module that provides
// distributed tracing via OpenTelemetry, exporting traces to any
// OTLP-compatible backend (Jaeger, Tempo, Honeycomb, etc.).
package tracing

import (
	"context"
	"fmt"
	"time"

	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/hook"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/yaml.v3"
)

func init() {
	core.RegisterModule(&Module{})
}

// Module is the hook.tracing module.
type Module struct {
	config   Config
	provider *sdktrace.TracerProvider
	tracer   trace.Tracer
	hooks    []hook.Hook
}

// Compile-time interface checks.
var (
	_ core.Module       = (*Module)(nil)
	_ core.Configurable = (*Module)(nil)
	_ core.Provisioner  = (*Module)(nil)
	_ core.Validator    = (*Module)(nil)
	_ core.Starter      = (*Module)(nil)
	_ core.Stopper      = (*Module)(nil)
	_ hook.Provider     = (*Module)(nil)
)

// ModuleInfo implements core.Module.
func (m *Module) ModuleInfo() core.ModuleInfo {
	return core.ModuleInfo{
		ID:  "hook.tracing",
		New: func() core.Module { return &Module{} },
	}
}

// Configure implements core.Configurable.
func (m *Module) Configure(node *yaml.Node) error {
	if err := node.Decode(&m.config); err != nil {
		return err
	}
	m.config.defaults()
	return nil
}

// Provision implements core.Provisioner.
func (m *Module) Provision(_ *core.AppContext) error {
	return nil
}

// Validate implements core.Validator.
func (m *Module) Validate() error {
	if m.config.Exporter != "otlp" {
		return fmt.Errorf("hook.tracing: only 'otlp' exporter is supported, got %q", m.config.Exporter)
	}
	if m.config.OTLP.Protocol != "http" && m.config.OTLP.Protocol != "grpc" {
		return fmt.Errorf("hook.tracing: protocol must be 'http' or 'grpc', got %q", m.config.OTLP.Protocol)
	}
	return nil
}

// Start implements core.Starter. It creates the OTLP exporter, TracerProvider,
// and registers hooks.
func (m *Module) Start() error {
	ctx, cancel := context.WithTimeout(context.Background(), m.config.OTLP.Timeout)
	defer cancel()

	exporter, err := m.createExporter(ctx)
	if err != nil {
		return fmt.Errorf("hook.tracing: creating exporter: %w", err)
	}

	res, err := m.buildResource(ctx)
	if err != nil {
		return fmt.Errorf("hook.tracing: building resource: %w", err)
	}

	sampler := sdktrace.TraceIDRatioBased(m.config.Sampling.Rate)

	m.provider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sampler)),
	)

	otel.SetTracerProvider(m.provider)
	m.tracer = m.provider.Tracer("sclaw")

	m.hooks = []hook.Hook{
		&startSpanHook{tracer: m.tracer},
		endSpanHook{},
	}

	return nil
}

// Stop implements core.Stopper. It flushes and shuts down the TracerProvider.
func (m *Module) Stop(ctx context.Context) error {
	if m.provider == nil {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return m.provider.Shutdown(shutdownCtx)
}

// Hooks implements hook.Provider.
func (m *Module) Hooks() []hook.Hook {
	return m.hooks
}

func (m *Module) createExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(m.config.OTLP.Endpoint),
		otlptracehttp.WithTimeout(m.config.OTLP.Timeout),
	}

	if m.config.OTLP.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	if len(m.config.OTLP.Headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(m.config.OTLP.Headers))
	}

	return otlptracehttp.New(ctx, opts...)
}

func (m *Module) buildResource(ctx context.Context) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(m.config.Service.Name),
	}
	if m.config.Service.Version != "" {
		attrs = append(attrs, semconv.ServiceVersion(m.config.Service.Version))
	}
	for k, v := range m.config.Service.Attributes {
		attrs = append(attrs, attribute.String(k, v))
	}

	return resource.New(ctx,
		resource.WithAttributes(attrs...),
		resource.WithProcessPID(),
		resource.WithHost(),
	)
}
