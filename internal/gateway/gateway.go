package gateway

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/router"
	"gopkg.in/yaml.v3"
)

func init() {
	core.RegisterModule(&Gateway{})
}

// Gateway is the HTTP gateway module. It exposes health, status, admin, and
// webhook endpoints. It is a leaf module — nothing imports it.
type Gateway struct {
	config     Config
	configPath string
	appCtx     *core.AppContext
	logger     *slog.Logger
	server     *http.Server
	metrics    *Metrics
	dispatcher *WebhookDispatcher
	startedAt  time.Time

	// Resolved lazily at Start() via service registry.
	sessions router.SessionStore
	chain    *provider.Chain
}

// ModuleInfo implements core.Module.
func (g *Gateway) ModuleInfo() core.ModuleInfo {
	return core.ModuleInfo{
		ID:  "gateway.http",
		New: func() core.Module { return &Gateway{} },
	}
}

// Configure implements core.Configurable.
func (g *Gateway) Configure(node *yaml.Node) error {
	if err := node.Decode(&g.config); err != nil {
		return err
	}
	g.config.defaults()
	return nil
}

// Provision implements core.Provisioner.
func (g *Gateway) Provision(ctx *core.AppContext) error {
	g.appCtx = ctx
	g.logger = ctx.Logger
	g.metrics = &Metrics{}
	g.dispatcher = NewWebhookDispatcher(g.logger)

	// Register services for cross-module discovery.
	ctx.RegisterService("gateway.metrics", g.metrics)
	ctx.RegisterService("gateway.webhook_dispatcher", g.dispatcher)

	// Configure webhook secrets from config.
	for source, cfg := range g.config.Webhooks {
		// Pre-register source entries so HMAC validation knows about secrets.
		// Actual handlers are registered by other modules via the dispatcher.
		if cfg.Secret != "" {
			g.logger.Info("webhook source configured", "source", source)
		}
	}

	// Try to resolve config path from appCtx data dir heuristic.
	// The config path is needed for the /api/config and /api/config/reload endpoints.
	g.configPath = g.config.configPath()

	return nil
}

// Validate implements core.Validator.
func (g *Gateway) Validate() error {
	if _, err := net.ResolveTCPAddr("tcp", g.config.Bind); err != nil {
		return errors.New("gateway: invalid bind address: " + g.config.Bind)
	}
	return nil
}

// Start implements core.Starter. It resolves dependencies from the service
// registry (lazy binding) and starts the HTTP server.
func (g *Gateway) Start() error {
	// Resolve optional services — graceful degradation if missing.
	if svc, ok := g.appCtx.Service("router.sessions"); ok {
		if store, ok := svc.(router.SessionStore); ok {
			g.sessions = store
		}
	}
	if svc, ok := g.appCtx.Service("provider.chain"); ok {
		if chain, ok := svc.(*provider.Chain); ok {
			g.chain = chain
		}
	}

	g.startedAt = time.Now()

	mux := g.buildRouter()

	g.server = &http.Server{
		Addr:         g.config.Bind,
		Handler:      mux,
		ReadTimeout:  g.config.ReadTimeout,
		WriteTimeout: g.config.WriteTimeout,
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", g.config.Bind)
	if err != nil {
		return errors.New("gateway: listen failed: " + err.Error())
	}

	go func() {
		g.logger.Info("gateway listening", "addr", g.config.Bind)
		if err := g.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			g.logger.Error("gateway serve error", "error", err)
		}
	}()

	return nil
}

// Stop implements core.Stopper. Graceful shutdown with configured timeout.
func (g *Gateway) Stop(ctx context.Context) error {
	if g.server == nil {
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, g.config.ShutdownTimeout)
	defer cancel()

	g.logger.Info("gateway shutting down")
	return g.server.Shutdown(shutdownCtx)
}

// configPath returns the config path if embedded in the gateway config.
// This is a placeholder — the actual config path is typically injected.
func (c *Config) configPath() string {
	return ""
}
