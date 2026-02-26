package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/flemzord/sclaw/internal/channel"
	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/multiagent"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/router"
	"github.com/flemzord/sclaw/internal/security"
	"github.com/flemzord/sclaw/internal/tool"
)

// routerModule wraps a *router.Router to satisfy core.Module, core.Starter,
// and core.Stopper, so the router participates in the App lifecycle.
type routerModule struct {
	router *router.Router
	ctx    context.Context
}

func (m *routerModule) ModuleInfo() core.ModuleInfo {
	return core.ModuleInfo{ID: "router"}
}

func (m *routerModule) Start() error {
	m.router.Start(m.ctx)
	return nil
}

func (m *routerModule) Stop(ctx context.Context) error {
	m.router.Stop(ctx)
	return nil
}

// wireRouter creates the Router, Dispatcher, and AgentFactory, wires them
// to every loaded channel, and appends the router to the app lifecycle.
// Must be called after LoadModules and before Start.
func wireRouter(
	app *core.App,
	appCtx *core.AppContext,
	ids []string,
	logger *slog.Logger,
	auditLogger *security.AuditLogger,
	rateLimiter *security.RateLimiter,
) error {
	// Discover channels and providers from loaded modules.
	dispatcher := channel.NewDispatcher()
	var channels []channel.Channel
	var defaultProvider provider.Provider

	for _, id := range ids {
		mod, ok := app.Module(id)
		if !ok {
			continue
		}
		if ch, ok := mod.(channel.Channel); ok {
			// Register under the full module ID (e.g. "channel.telegram") because
			// that is what the channel sets as msg.Channel in inbound messages.
			if err := dispatcher.Register(id, ch); err != nil {
				return fmt.Errorf("registering channel %s: %w", id, err)
			}
			channels = append(channels, ch)
			logger.Info("router: registered channel", "channel", id)
		}
		if p, ok := mod.(provider.Provider); ok {
			defaultProvider = p
			logger.Info("router: discovered provider", "module", id)
		}
	}

	if len(channels) == 0 {
		logger.Info("router: no channels found, skipping router wiring")
		return nil
	}

	if defaultProvider == nil {
		return fmt.Errorf("router: at least one provider module is required")
	}

	// Resolve multiagent registry; create a default one for single-agent setups.
	var registry *multiagent.Registry
	if svc, ok := appCtx.GetService("multiagent.registry"); ok {
		registry, _ = svc.(*multiagent.Registry)
	}
	if registry == nil {
		agents := map[string]multiagent.AgentConfig{
			"default": {
				Workspace: appCtx.Workspace,
				Routing:   multiagent.RoutingConfig{Default: true},
			},
		}
		var err error
		registry, err = multiagent.NewRegistry(agents, []string{"default"})
		if err != nil {
			return fmt.Errorf("creating default agent registry: %w", err)
		}
	}

	// Resolve URL filter (optional).
	var urlFilter *security.URLFilter
	if svc, ok := appCtx.GetService("security.urlfilter"); ok {
		urlFilter, _ = svc.(*security.URLFilter)
	}

	// Build the agent factory.
	factory := multiagent.NewFactory(multiagent.FactoryConfig{
		Registry:        registry,
		DefaultProvider: defaultProvider,
		GlobalTools:     tool.NewRegistry(),
		Logger:          logger,
		AuditLogger:     auditLogger,
		RateLimiter:     rateLimiter,
		URLFilter:       urlFilter,
	})

	// Create the router.
	r, err := router.NewRouter(router.Config{
		AgentFactory:   factory,
		ResponseSender: dispatcher,
		Logger:         logger,
		RateLimiter:    rateLimiter,
	})
	if err != nil {
		return fmt.Errorf("creating router: %w", err)
	}

	// Wire each channel's inbox to the router.
	for _, ch := range channels {
		ch.SetInbox(r.Submit)
	}

	// Append the router to the app lifecycle.
	app.AppendModule("router", &routerModule{
		router: r,
		ctx:    context.Background(),
	})

	// Register the session store for the gateway to discover.
	appCtx.RegisterService("router.sessions", r.Sessions())

	logger.Info("router: wired", "channels", len(channels))
	return nil
}
