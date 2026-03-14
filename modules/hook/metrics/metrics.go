// Package metrics implements the hook.metrics module that collects
// observability metrics on every agent interaction and exposes them
// via a Prometheus /metrics endpoint on the existing gateway.
package metrics

import (
	"fmt"
	"net/http"

	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/hook"
	"github.com/prometheus/client_golang/prometheus"
	promcollectors "github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/yaml.v3"
)

func init() {
	core.RegisterModule(&Module{})
}

// Module is the hook.metrics module.
type Module struct {
	config     Config
	registry   *prometheus.Registry
	collectors *collectors
	handler    http.Handler
	hooks      []hook.Hook
}

// Compile-time interface checks.
var (
	_ core.Module       = (*Module)(nil)
	_ core.Configurable = (*Module)(nil)
	_ core.Provisioner  = (*Module)(nil)
	_ core.Validator    = (*Module)(nil)
	_ hook.Provider     = (*Module)(nil)
)

// ModuleInfo implements core.Module.
func (m *Module) ModuleInfo() core.ModuleInfo {
	return core.ModuleInfo{
		ID:  "hook.metrics",
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
func (m *Module) Provision(ctx *core.AppContext) error {
	m.registry = prometheus.NewRegistry()
	m.registry.MustRegister(promcollectors.NewGoCollector())
	m.registry.MustRegister(promcollectors.NewProcessCollector(promcollectors.ProcessCollectorOpts{}))

	m.collectors = newCollectors(m.registry)

	m.handler = promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})

	m.hooks = []hook.Hook{
		timingHook{},
		&metricsHook{collectors: m.collectors, config: &m.config},
	}

	// Register the handler and path for the gateway to mount.
	ctx.RegisterService("hook.metrics.handler", m.handler)
	ctx.RegisterService("hook.metrics.path", m.config.Prometheus.Path)

	// Register active sessions gauge for the router to update.
	ctx.RegisterService("hook.metrics.active_sessions", m.collectors.activeSessions)

	return nil
}

// Validate implements core.Validator.
func (m *Module) Validate() error {
	if m.config.Exporter != "prometheus" {
		return fmt.Errorf("hook.metrics: only 'prometheus' exporter is supported, got %q", m.config.Exporter)
	}
	return nil
}

// Hooks implements hook.Provider.
func (m *Module) Hooks() []hook.Hook {
	return m.hooks
}
