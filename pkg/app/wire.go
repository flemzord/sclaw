package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/flemzord/sclaw/internal/channel"
	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/cron"
	"github.com/flemzord/sclaw/internal/multiagent"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/router"
	"github.com/flemzord/sclaw/internal/security"
	"github.com/flemzord/sclaw/internal/tool"
)

// factoryCloser is implemented by multiagent.Factory to close SQLite databases.
type factoryCloser interface {
	Close() error
}

// routerModule wraps a *router.Router to satisfy core.Module, core.Starter,
// and core.Stopper, so the router participates in the App lifecycle.
type routerModule struct {
	router  *router.Router
	factory factoryCloser
	ctx     context.Context
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
	if m.factory != nil {
		return m.factory.Close()
	}
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
		multiagent.ResolveDefaults(agents, appCtx.DataDir)
		if err := multiagent.EnsureDirectories(agents); err != nil {
			return fmt.Errorf("creating default agent directories: %w", err)
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
		AgentFactory:    factory,
		ResponseSender:  dispatcher,
		ChannelLookup:   dispatcher,
		Logger:          logger,
		RateLimiter:     rateLimiter,
		HistoryResolver: factory,
		SoulResolver:    factory,
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
		router:  r,
		factory: factory,
		ctx:     context.Background(),
	})

	// Register the session store for the gateway to discover.
	appCtx.RegisterService("router.sessions", r.Sessions())

	logger.Info("router: wired", "channels", len(channels))
	return nil
}

// schedulerModule wraps a *cron.Scheduler to satisfy core.Module, core.Starter,
// and core.Stopper, so the scheduler participates in the App lifecycle.
type schedulerModule struct {
	scheduler *cron.Scheduler
}

func (m *schedulerModule) ModuleInfo() core.ModuleInfo {
	return core.ModuleInfo{ID: "cron"}
}

func (m *schedulerModule) Start() error {
	return m.scheduler.Start()
}

func (m *schedulerModule) Stop(ctx context.Context) error {
	return m.scheduler.Stop(ctx)
}

// wireCron creates the cron Scheduler, registers background jobs, and appends
// the scheduler to the app lifecycle. Must be called after wireRouter (needs
// "router.sessions") and before Start.
func wireCron(
	app *core.App,
	appCtx *core.AppContext,
	logger *slog.Logger,
) error {
	s := cron.NewScheduler(logger)

	// Wire session cleanup if the router registered a session store.
	if svc, ok := appCtx.GetService("router.sessions"); ok {
		if store, ok := svc.(cron.SessionStore); ok {
			if err := s.RegisterJob(&cron.SessionCleanupJob{
				Store:   store,
				MaxIdle: 30 * time.Minute,
				Logger:  logger,
			}); err != nil {
				return fmt.Errorf("cron: registering session cleanup: %w", err)
			}
		}
	}

	// Register stub memory jobs (no-op until the memory subsystem is fully wired).
	if err := s.RegisterJob(&cron.MemoryExtractionJob{Logger: logger}); err != nil {
		return fmt.Errorf("cron: registering memory extraction: %w", err)
	}
	if err := s.RegisterJob(&cron.MemoryCompactionJob{Logger: logger}); err != nil {
		return fmt.Errorf("cron: registering memory compaction: %w", err)
	}

	app.AppendModule("cron", &schedulerModule{scheduler: s})
	logger.Info("cron: wired")
	return nil
}
