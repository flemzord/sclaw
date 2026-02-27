package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/flemzord/sclaw/internal/channel"
	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/cron"
	"github.com/flemzord/sclaw/internal/memory"
	"github.com/flemzord/sclaw/internal/multiagent"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/router"
	"github.com/flemzord/sclaw/internal/security"
	"github.com/flemzord/sclaw/internal/subagent"
	"github.com/flemzord/sclaw/internal/tool"
)

// factoryCloser is implemented by multiagent.Factory to close SQLite databases.
type factoryCloser interface {
	Close() error
}

// routerModule wraps a *router.Router to satisfy core.Module, core.Starter,
// and core.Stopper, so the router participates in the App lifecycle.
type routerModule struct {
	router      *router.Router
	factory     factoryCloser
	subagentMgr *subagent.Manager
	ctx         context.Context
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
	if m.subagentMgr != nil {
		m.subagentMgr.Shutdown(ctx)
	}
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

	// Resolve memory module's history store (if any).
	var historyStore memory.HistoryStore
	if svc, ok := appCtx.GetService("memory.history"); ok {
		historyStore, _ = svc.(memory.HistoryStore)
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
		HistoryStore:    historyStore,
	})

	// Create sub-agent manager and wire it into the factory.
	loopFactory := multiagent.NewSubAgentLoopFactory(defaultProvider, factory.GlobalTools())
	subMgr := subagent.NewManager(subagent.ManagerConfig{
		Logger:      logger,
		LoopFactory: loopFactory,
	})
	factory.SetSubAgentManager(subMgr)

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
		router:      r,
		factory:     factory,
		subagentMgr: subMgr,
		ctx:         context.Background(),
	})

	// Register the session store for the gateway to discover.
	appCtx.RegisterService("router.sessions", r.Sessions())

	// Register the default provider for use by cron jobs (e.g. fact extraction).
	appCtx.RegisterService("provider.default", defaultProvider)

	logger.Info("router: wired", "channels", len(channels))
	return nil
}

// rangeableSessionStore is the subset of router.SessionStore needed to iterate
// sessions for cron jobs. Defined locally to avoid exporting a wide interface.
type rangeableSessionStore interface {
	Range(func(router.SessionKey, *router.Session) bool)
}

// sessionRangerAdapter bridges rangeableSessionStore to cron.SessionRanger.
// It derives the session ID using the same persistenceKey formula as the
// router pipeline, so the cron job looks up sessions by the same key
// used to persist messages in the HistoryStore.
type sessionRangerAdapter struct{ store rangeableSessionStore }

func (a *sessionRangerAdapter) Range(fn func(sessionID, agentID string) bool) {
	a.store.Range(func(key router.SessionKey, s *router.Session) bool {
		pKey := key.Channel + ":" + key.ChatID + ":" + key.ThreadID
		return fn(pKey, s.AgentID)
	})
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
//
// When a multiagent registry is available, jobs are registered per-agent using
// each agent's CronConfig. Otherwise, global jobs with defaults are registered
// for single-agent setups.
func wireCron(
	app *core.App,
	appCtx *core.AppContext,
	logger *slog.Logger,
) error {
	s := cron.NewScheduler(logger)

	// Resolve the session store (optional — only available when the router is wired).
	var sessionStore cron.SessionStore
	var ranger cron.SessionRanger
	if svc, ok := appCtx.GetService("router.sessions"); ok {
		sessionStore, _ = svc.(cron.SessionStore)
		if rs, ok := svc.(rangeableSessionStore); ok {
			ranger = &sessionRangerAdapter{store: rs}
		}
	}

	// Resolve memory dependencies for fact extraction.
	var historyStore memory.HistoryStore
	if svc, ok := appCtx.GetService("memory.history"); ok {
		historyStore, _ = svc.(memory.HistoryStore)
	}
	var memoryStore memory.Store
	if svc, ok := appCtx.GetService("memory.store"); ok {
		memoryStore, _ = svc.(memory.Store)
	}
	var extractor memory.FactExtractor
	if svc, ok := appCtx.GetService("provider.default"); ok {
		if p, ok := svc.(provider.Provider); ok {
			extractor = memory.NewLLMExtractor(p)
		}
	}

	// Resolve multiagent registry for per-agent job configuration.
	var registry *multiagent.Registry
	if svc, ok := appCtx.GetService("multiagent.registry"); ok {
		registry, _ = svc.(*multiagent.Registry)
	}

	if registry != nil {
		// Per-agent jobs: one set of jobs per registered agent.
		for _, agentID := range registry.AgentIDs() {
			cfg, _ := registry.AgentConfig(agentID)
			cronCfg := cfg.Cron

			if sessionStore != nil {
				if err := s.RegisterJob(&cron.SessionCleanupJob{
					Store:        sessionStore,
					MaxIdle:      cronCfg.SessionCleanup.MaxIdleOrDefault(),
					Logger:       logger,
					AgentID:      agentID,
					ScheduleExpr: cronCfg.SessionCleanup.ScheduleOrDefault(),
				}); err != nil {
					return fmt.Errorf("cron: registering session cleanup for agent %s: %w", agentID, err)
				}
			}

			if err := s.RegisterJob(&cron.MemoryExtractionJob{
				Logger:       logger,
				AgentID:      agentID,
				ScheduleExpr: cronCfg.MemoryExtraction.ScheduleOrDefault(),
				Sessions:     ranger,
				History:      historyStore,
				Store:        memoryStore,
				Extractor:    extractor,
			}); err != nil {
				return fmt.Errorf("cron: registering memory extraction for agent %s: %w", agentID, err)
			}

			if err := s.RegisterJob(&cron.MemoryCompactionJob{
				Logger:       logger,
				AgentID:      agentID,
				ScheduleExpr: cronCfg.MemoryCompaction.ScheduleOrDefault(),
			}); err != nil {
				return fmt.Errorf("cron: registering memory compaction for agent %s: %w", agentID, err)
			}
		}
	} else {
		// Single-agent fallback: global jobs with defaults.
		if sessionStore != nil {
			if err := s.RegisterJob(&cron.SessionCleanupJob{
				Store:   sessionStore,
				MaxIdle: 30 * time.Minute,
				Logger:  logger,
			}); err != nil {
				return fmt.Errorf("cron: registering session cleanup: %w", err)
			}
		}

		if err := s.RegisterJob(&cron.MemoryExtractionJob{
			Logger:    logger,
			Sessions:  ranger,
			History:   historyStore,
			Store:     memoryStore,
			Extractor: extractor,
		}); err != nil {
			return fmt.Errorf("cron: registering memory extraction: %w", err)
		}
		if err := s.RegisterJob(&cron.MemoryCompactionJob{Logger: logger}); err != nil {
			return fmt.Errorf("cron: registering memory compaction: %w", err)
		}
	}

	app.AppendModule("cron", &schedulerModule{scheduler: s})
	logger.Info("cron: wired")
	return nil
}
