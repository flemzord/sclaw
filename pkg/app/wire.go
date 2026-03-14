package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/flemzord/sclaw/internal/channel"
	"github.com/flemzord/sclaw/internal/config"
	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/cron"
	"github.com/flemzord/sclaw/internal/hook"
	"github.com/flemzord/sclaw/internal/memory"
	"github.com/flemzord/sclaw/internal/multiagent"
	"github.com/flemzord/sclaw/internal/provider"
	"github.com/flemzord/sclaw/internal/reload"
	"github.com/flemzord/sclaw/internal/router"
	"github.com/flemzord/sclaw/internal/security"
	"github.com/flemzord/sclaw/internal/subagent"
	"github.com/flemzord/sclaw/internal/tool"
	"github.com/flemzord/sclaw/internal/tool/builtin"
	"github.com/flemzord/sclaw/internal/tool/configtool"
	"github.com/flemzord/sclaw/internal/tool/crontool"
	"github.com/flemzord/sclaw/pkg/message"
	"github.com/flemzord/sclaw/skills"
)

// routerModule wraps a *router.Router to satisfy core.Module, core.Starter,
// core.Stopper, and core.Reloader, so the router participates in the App lifecycle.
type routerModule struct {
	router      *router.Router
	factory     *multiagent.Factory
	subagentMgr *subagent.Manager
	ctx         context.Context
	logger      *slog.Logger
	dataDir     string
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

// Reload parses new agent configs, builds a fresh Registry, and atomically
// swaps it into the Factory. If AgentConfigs is nil, reload is a no-op.
func (m *routerModule) Reload(ctx *core.AppContext) error {
	agents := ctx.AgentConfigs()
	if agents == nil {
		return nil
	}

	parsed, order, err := multiagent.ParseAgents(agents)
	if err != nil {
		return fmt.Errorf("router: parsing agents: %w", err)
	}

	multiagent.ResolveDefaults(parsed, m.dataDir)

	if err := multiagent.EnsureDirectories(parsed); err != nil {
		return fmt.Errorf("router: creating agent directories: %w", err)
	}

	newRegistry, err := multiagent.NewRegistry(parsed, order)
	if err != nil {
		return fmt.Errorf("router: creating registry: %w", err)
	}

	m.factory.Reload(newRegistry)
	m.logger.Info("router: agent configuration reloaded",
		"agents", len(parsed),
	)
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
	routerCfg *config.RouterConfig,
) error {
	// Discover channels and providers from loaded modules.
	dispatcher := channel.NewDispatcher()
	var channels []channel.Channel
	var defaultProvider provider.Provider
	var defaultProviderName string

	// Hook pipeline for before_process / before_send / after_send hooks.
	hookPipeline := hook.NewPipeline()

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
			defaultProviderName = id
			logger.Info("router: discovered provider", "module", id)
		}
		if hp, ok := mod.(hook.Provider); ok {
			for _, h := range hp.Hooks() {
				hookPipeline.Register(h)
				logger.Info("router: registered hook",
					"module", id,
					"position", h.Position(),
					"priority", h.Priority(),
				)
			}
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

	// Build the global tool registry.
	// First, discover tools from ToolProvider modules (configurable replacements).
	// Then, register built-in tools only for names not already covered by a module.
	globalTools := tool.NewRegistry()

	coveredTools := make(map[string]bool)
	for _, id := range ids {
		mod, ok := app.Module(id)
		if !ok {
			continue
		}
		if tp, ok := mod.(tool.Provider); ok {
			for _, t := range tp.Tools() {
				if err := globalTools.Register(t); err != nil {
					return fmt.Errorf("registering tool from module %s: %w", id, err)
				}
				coveredTools[t.Name()] = true
				logger.Info("router: registered tool from module", "tool", t.Name(), "module", id)
			}
		}
	}

	// Register built-in tools that are NOT covered by any module.
	for _, t := range builtin.All() {
		if coveredTools[t.Name()] {
			continue
		}
		if err := globalTools.Register(t); err != nil {
			return fmt.Errorf("registering built-in tool %s: %w", t.Name(), err)
		}
	}

	// Register config tools (read/modify sclaw.yaml at runtime).
	var cfgPath string
	if svc, ok := appCtx.GetService("config.path"); ok {
		cfgPath, _ = svc.(string)
	}
	var redactor *security.Redactor
	if svc, ok := appCtx.GetService("security.redactor"); ok {
		redactor, _ = svc.(*security.Redactor)
	}
	if cfgPath != "" {
		if err := configtool.RegisterAll(globalTools, configtool.Deps{
			ConfigPath: cfgPath,
			Redactor:   redactor,
			ReloadFn: func(ctx context.Context, path string) error {
				if svc, ok := appCtx.GetService("reload.handler"); ok {
					if h, ok := svc.(*reload.Handler); ok {
						return h.HandleReload(ctx, path)
					}
				}
				return fmt.Errorf("reload handler not available")
			},
		}); err != nil {
			return fmt.Errorf("registering config tools: %w", err)
		}
	}

	// Resolve sanitized environment for subprocess tools.
	var sanitizedEnv []string
	if svc, ok := appCtx.GetService("security.sanitized_env"); ok {
		sanitizedEnv, _ = svc.([]string)
	}

	// Build the agent factory.
	globalSkillsDir := filepath.Join(appCtx.DataDir, "skills")

	factory := multiagent.NewFactory(multiagent.FactoryConfig{
		Registry:            registry,
		DefaultProvider:     defaultProvider,
		DefaultProviderName: defaultProviderName,
		GlobalTools:         globalTools,
		Logger:              logger,
		AuditLogger:         auditLogger,
		RateLimiter:         rateLimiter,
		URLFilter:           urlFilter,
		SanitizedEnv:        sanitizedEnv,
		BuiltinSkillsFS:     skills.BuiltinFS,
		GlobalSkillsDir:     globalSkillsDir,
	})

	// Create sub-agent manager and wire it into the factory.
	loopFactory := multiagent.NewSubAgentLoopFactory(defaultProvider, factory.GlobalTools())
	subMgr := subagent.NewManager(subagent.ManagerConfig{
		Logger:      logger,
		LoopFactory: loopFactory,
	})
	factory.SetSubAgentManager(subMgr)

	// Build group policy from config.
	var groupPolicy router.GroupPolicy
	if routerCfg != nil {
		groupPolicy = router.GroupPolicy{
			Mode:      router.GroupPolicyMode(routerCfg.GroupPolicy.Mode),
			Allowlist: routerCfg.GroupPolicy.Allowlist,
			Denylist:  routerCfg.GroupPolicy.Denylist,
		}
	}

	// Create the router.
	r, err := router.NewRouter(router.Config{
		AgentFactory:    factory,
		ResponseSender:  dispatcher,
		ChannelLookup:   dispatcher,
		StreamSender:    dispatcher,
		GroupPolicy:     groupPolicy,
		Logger:          logger,
		RateLimiter:     rateLimiter,
		HookPipeline:    hookPipeline,
		HistoryResolver: factory,
		SoulResolver:    factory,
		SkillResolver:   factory,
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
		logger:      logger,
		dataDir:     appCtx.DataDir,
	})

	// Register the session store for the gateway to discover.
	appCtx.RegisterService("router.sessions", r.Sessions())

	// Register the default provider for use by cron jobs (e.g. fact extraction).
	appCtx.RegisterService("provider.default", defaultProvider)

	// Register factory and dispatcher for prompt cron wiring.
	appCtx.RegisterService("multiagent.factory", factory)
	appCtx.RegisterService("channel.dispatcher", dispatcher)

	// Register cron CRUD tools for runtime cron management.
	if err := crontool.RegisterAll(globalTools, crontool.Deps{
		ReloadFn: func() error {
			if svc, ok := appCtx.GetService("reload.handler"); ok {
				if h, ok := svc.(*reload.Handler); ok {
					return h.HandleReload(context.Background(), cfgPath)
				}
			}
			return nil
		},
	}); err != nil {
		return fmt.Errorf("registering cron tools: %w", err)
	}

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

// cronOutputAdapter bridges channel.Dispatcher to cron.OutputSender.
type cronOutputAdapter struct {
	dispatcher *channel.Dispatcher
}

func (a *cronOutputAdapter) SendCronOutput(ctx context.Context, ch, chatID, text string) error {
	return a.dispatcher.Send(ctx, message.OutboundMessage{
		Channel: ch,
		Chat:    message.Chat{ID: chatID, Type: message.ChatDM},
		Blocks:  []message.ContentBlock{message.NewTextBlock(text)},
	})
}

// schedulerModule wraps a *cron.Scheduler to satisfy core.Module, core.Starter,
// core.Stopper, and core.Reloader, so the scheduler participates in the App lifecycle.
type schedulerModule struct {
	scheduler    *cron.Scheduler
	logger       *slog.Logger
	dataDir      string
	sessionStore cron.SessionStore
	ranger       cron.SessionRanger
	factory      *multiagent.Factory
	extractor    memory.FactExtractor
	loopBuilder  cron.LoopBuilder
	outputSender cron.OutputSender
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

// Reload stops the current scheduler, creates a new one with jobs derived
// from the new agent configs, and starts it. If AgentConfigs is nil, reload
// is a no-op.
func (m *schedulerModule) Reload(ctx *core.AppContext) error {
	agents := ctx.AgentConfigs()
	if agents == nil {
		return nil
	}

	parsed, order, err := multiagent.ParseAgents(agents)
	if err != nil {
		return fmt.Errorf("cron: parsing agents: %w", err)
	}

	multiagent.ResolveDefaults(parsed, m.dataDir)

	registry, err := multiagent.NewRegistry(parsed, order)
	if err != nil {
		return fmt.Errorf("cron: creating registry: %w", err)
	}

	// Stop old scheduler with a 10s timeout.
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.scheduler.Stop(stopCtx); err != nil {
		m.logger.Error("cron: error stopping scheduler during reload", "error", err)
	}

	// Create and populate new scheduler.
	newScheduler := cron.NewScheduler(m.logger)

	for _, agentID := range registry.AgentIDs() {
		cfg, _ := registry.AgentConfig(agentID)
		cronCfg := cfg.Cron

		if m.sessionStore != nil {
			if err := newScheduler.RegisterJob(&cron.SessionCleanupJob{
				Store:        m.sessionStore,
				MaxIdle:      cronCfg.SessionCleanup.MaxIdleOrDefault(),
				Logger:       m.logger,
				AgentID:      agentID,
				ScheduleExpr: cronCfg.SessionCleanup.ScheduleOrDefault(),
			}); err != nil {
				return fmt.Errorf("cron: registering session cleanup for agent %s: %w", agentID, err)
			}
		}

		var agentHistory memory.HistoryStore
		var agentFactStore memory.Store
		if m.factory != nil {
			agentHistory = m.factory.ResolveHistory(agentID)
			agentFactStore = m.factory.ResolveFactStore(agentID)
		}

		if err := newScheduler.RegisterJob(&cron.MemoryExtractionJob{
			Logger:       m.logger,
			AgentID:      agentID,
			ScheduleExpr: cronCfg.MemoryExtraction.ScheduleOrDefault(),
			Sessions:     m.ranger,
			History:      agentHistory,
			Store:        agentFactStore,
			Extractor:    m.extractor,
		}); err != nil {
			return fmt.Errorf("cron: registering memory extraction for agent %s: %w", agentID, err)
		}

		if err := newScheduler.RegisterJob(&cron.MemoryCompactionJob{
			Logger:       m.logger,
			AgentID:      agentID,
			ScheduleExpr: cronCfg.MemoryCompaction.ScheduleOrDefault(),
		}); err != nil {
			return fmt.Errorf("cron: registering memory compaction for agent %s: %w", agentID, err)
		}

		// Register prompt crons for this agent.
		if m.loopBuilder != nil {
			cronsDir := cron.CronsDir(cfg.DataDir)
			defs, errs := cron.ScanPromptCrons(cronsDir)
			for _, e := range errs {
				m.logger.Error("cron: invalid prompt cron", "agent", agentID, "error", e)
			}
			for _, def := range defs {
				if err := newScheduler.RegisterJob(&cron.PromptJob{
					Def:     def,
					AgentID: agentID,
					Builder: m.loopBuilder,
					Sender:  m.outputSender,
					DataDir: cfg.DataDir,
					Logger:  m.logger,
				}); err != nil {
					m.logger.Error("cron: registering prompt cron",
						"agent", agentID, "cron", def.Name, "error", err)
				}
			}
		}
	}

	if err := newScheduler.Start(); err != nil {
		return fmt.Errorf("cron: starting new scheduler: %w", err)
	}

	m.scheduler = newScheduler
	m.logger.Info("cron: scheduler reloaded", "agents", len(parsed))
	return nil
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

	// Resolve factory for per-agent memory stores (optional — only when router is wired).
	var factory *multiagent.Factory
	if svc, ok := appCtx.GetService("multiagent.factory"); ok {
		factory, _ = svc.(*multiagent.Factory)
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

	// Resolve LoopBuilder and OutputSender for prompt crons.
	var loopBuilder cron.LoopBuilder
	if svc, ok := appCtx.GetService("multiagent.factory"); ok {
		loopBuilder, _ = svc.(cron.LoopBuilder)
	}
	var outputSender cron.OutputSender
	if svc, ok := appCtx.GetService("channel.dispatcher"); ok {
		if d, ok := svc.(*channel.Dispatcher); ok {
			outputSender = &cronOutputAdapter{dispatcher: d}
		}
	}

	// Create a CronTrigger so the gateway can list and fire prompt crons.
	cronTrigger := cron.NewTrigger()

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

			var agentHistory memory.HistoryStore
			var agentFactStore memory.Store
			if factory != nil {
				agentHistory = factory.ResolveHistory(agentID)
				agentFactStore = factory.ResolveFactStore(agentID)
			}

			if err := s.RegisterJob(&cron.MemoryExtractionJob{
				Logger:       logger,
				AgentID:      agentID,
				ScheduleExpr: cronCfg.MemoryExtraction.ScheduleOrDefault(),
				Sessions:     ranger,
				History:      agentHistory,
				Store:        agentFactStore,
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

			// Register prompt crons for this agent.
			if loopBuilder != nil {
				cronsDir := cron.CronsDir(cfg.DataDir)
				defs, errs := cron.ScanPromptCrons(cronsDir)
				for _, e := range errs {
					logger.Error("cron: invalid prompt cron", "agent", agentID, "error", e)
				}
				for _, def := range defs {
					job := &cron.PromptJob{
						Def:     def,
						AgentID: agentID,
						Builder: loopBuilder,
						Sender:  outputSender,
						DataDir: cfg.DataDir,
						Logger:  logger,
					}
					if err := s.RegisterJob(job); err != nil {
						logger.Error("cron: registering prompt cron",
							"agent", agentID, "cron", def.Name, "error", err)
					} else {
						cronTrigger.Register(job)
					}
				}
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

		// Single-agent fallback: resolve stores for "default" agent via factory.
		var defaultHistory memory.HistoryStore
		var defaultFactStore memory.Store
		if factory != nil {
			defaultHistory = factory.ResolveHistory("default")
			defaultFactStore = factory.ResolveFactStore("default")
		}

		if err := s.RegisterJob(&cron.MemoryExtractionJob{
			Logger:    logger,
			Sessions:  ranger,
			History:   defaultHistory,
			Store:     defaultFactStore,
			Extractor: extractor,
		}); err != nil {
			return fmt.Errorf("cron: registering memory extraction: %w", err)
		}
		if err := s.RegisterJob(&cron.MemoryCompactionJob{Logger: logger}); err != nil {
			return fmt.Errorf("cron: registering memory compaction: %w", err)
		}
	}

	// Register CronTrigger as a service for the gateway to discover.
	appCtx.RegisterService("cron.trigger", cronTrigger)

	app.AppendModule("cron", &schedulerModule{
		scheduler:    s,
		logger:       logger,
		dataDir:      appCtx.DataDir,
		sessionStore: sessionStore,
		ranger:       ranger,
		factory:      factory,
		extractor:    extractor,
		loopBuilder:  loopBuilder,
		outputSender: outputSender,
	})
	logger.Info("cron: wired")
	return nil
}
