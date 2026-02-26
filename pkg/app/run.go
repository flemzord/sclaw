// Package app provides the shared entry point for sclaw and xsclaw binaries.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/flemzord/sclaw/internal/bootstrap"
	"github.com/flemzord/sclaw/internal/config"
	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/internal/multiagent"
	"github.com/flemzord/sclaw/internal/reload"
	"github.com/flemzord/sclaw/internal/security"
)

// RunParams configures the main application loop.
type RunParams struct {
	// ConfigPath is an explicit path to the YAML configuration file.
	// If empty, ResolveConfigPath is called automatically.
	ConfigPath string

	// Version, Commit, and Date are injected at build time via ldflags.
	Version string
	Commit  string
	Date    string

	// DataDir overrides the default persistent data directory.
	DataDir string

	// Workspace overrides the default working directory.
	Workspace string

	// LogLevel sets the minimum log level. Defaults to slog.LevelInfo.
	LogLevel slog.Level

	// BuildHash is the SHA-256 hash of the compiled plugin list, injected by
	// xsclaw via ldflags. When non-empty the bootstrapper checks for plugin
	// changes on every config reload and triggers a rebuild + re-exec when
	// the desired set diverges from the compiled set.
	BuildHash string
}

// Run loads configuration, starts all modules, and blocks until a shutdown
// signal is received. SIGHUP and file-change events trigger a live
// configuration reload for modules that implement core.Reloader.
func Run(params RunParams) error {
	cfgPath := params.ConfigPath
	if cfgPath == "" {
		resolved, err := ResolveConfigPath()
		if err != nil {
			return err
		}
		cfgPath = resolved
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	if err := config.Validate(cfg); err != nil {
		return err
	}

	// Initialize credential store and redactor (security foundation).
	credStore := security.NewCredentialStore()
	redactor := security.NewRedactor()

	// Wrap the text handler in a redacting handler to prevent secret leakage in logs.
	innerHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: params.LogLevel,
	})
	logger := slog.New(security.NewRedactingHandler(innerHandler, redactor))

	// Initialize audit logger.
	auditLogger := security.NewAuditLogger(security.AuditLoggerConfig{
		Redactor: redactor,
	})

	// Initialize rate limiter from config if present.
	var rateLimiter *security.RateLimiter
	if cfg.Security != nil {
		rl := cfg.Security.RateLimits
		rateLimiter = security.NewRateLimiter(security.RateLimitConfig{
			MaxSessions:     rl.MaxSessions,
			MessagesPerMin:  rl.MessagesPerMin,
			ToolCallsPerMin: rl.ToolCallsPerMin,
			TokensPerHour:   rl.TokensPerHour,
		})
	} else {
		rateLimiter = security.NewRateLimiter(security.RateLimitConfig{})
	}

	dataDir := params.DataDir
	if dataDir == "" {
		dataDir = DefaultDataDir()
	}
	workspace := params.Workspace
	if workspace == "" {
		workspace = DefaultWorkspace()
	}

	appCtx := core.NewAppContext(logger, dataDir, workspace)
	appCtx = appCtx.WithModuleConfigs(cfg.Modules)

	// Register security services for cross-module discovery.
	appCtx.RegisterService("security.credentials", credStore)
	appCtx.RegisterService("security.redactor", redactor)
	appCtx.RegisterService("security.audit", auditLogger)
	appCtx.RegisterService("security.ratelimiter", rateLimiter)

	// Parse and register multi-agent configuration if present.
	if len(cfg.Agents) > 0 {
		agents, order, err := multiagent.ParseAgents(cfg.Agents)
		if err != nil {
			return err
		}
		multiagent.ResolveDefaults(agents, dataDir)
		if err := multiagent.EnsureDirectories(agents); err != nil {
			return err
		}
		registry, err := multiagent.NewRegistry(agents, order)
		if err != nil {
			return err
		}
		appCtx.RegisterService("multiagent.registry", registry)
		appCtx.RegisterService("multiagent.agents", agents)
		logger.Info("multi-agent mode enabled", "agents", len(agents))
	}

	// Register the config path so modules (e.g. the gateway) can discover it.
	appCtx.RegisterService("config.path", cfgPath)

	// Build and register URL filter if configured.
	if cfg.Security != nil && len(cfg.Security.URLFilter.AllowDomains) > 0 {
		urlFilter := security.NewURLFilter(security.URLFilterConfig{
			AllowDomains: cfg.Security.URLFilter.AllowDomains,
			DenyDomains:  cfg.Security.URLFilter.DenyDomains,
		})
		appCtx.RegisterService("security.urlfilter", urlFilter)
	}

	application := core.NewApp(appCtx)
	ids := config.Resolve(cfg)
	if err := application.LoadModules(ids); err != nil {
		return err
	}

	// Wire the router between LoadModules and Start: discover channels and
	// providers, create the dispatcher and agent factory, call SetInbox on
	// every channel, and append the router to the app lifecycle.
	if err := wireRouter(application, appCtx, ids, logger, auditLogger, rateLimiter); err != nil {
		return err
	}

	// Build and register the reload handler BEFORE Start so gateway can use it.
	handler := reload.NewHandler(application, logger, dataDir, workspace)
	appCtx.RegisterService("reload.handler", handler)

	if err := application.Start(); err != nil {
		return err
	}

	// Sync the redactor with all credentials registered by modules during Start.
	// This ensures runtime secrets (e.g. API keys loaded from env by modules)
	// are redacted from logs going forward.
	redactor.SyncCredentials(credStore)

	// Build sanitized environment after modules have registered their credentials,
	// and make it available for tool execution via service lookup.
	appCtx.RegisterService("security.sanitized_env", security.SanitizedEnv(credStore))

	// --- signal handling ---
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	// --- file watcher ---
	watcher := reload.NewWatcher(reload.WatcherConfig{
		ConfigPath: cfgPath,
	})
	watchCtx, watchCancel := context.WithCancel(context.Background())
	defer watchCancel()
	watcher.Start(watchCtx)
	defer watcher.Stop()

	// --- bootstrapper (optional, only for xsclaw-built binaries) ---
	var bs *bootstrap.Bootstrapper
	if params.BuildHash != "" {
		var bsErr error
		bs, bsErr = bootstrap.NewBootstrapper(params.BuildHash)
		if bsErr != nil {
			logger.Warn("bootstrapper unavailable, hot plugin reload disabled", "error", bsErr)
		}
	}

	// --- main event loop ---
	for {
		select {
		case sig := <-sigCh:
			switch sig {
			case syscall.SIGHUP:
				logger.Info("SIGHUP received, reloading configuration")
				if err := reloadOrRebuild(watchCtx, logger, handler, bs, application, cfgPath); err != nil {
					logger.Error("reload failed", "error", err)
				}
			default:
				logger.Info("shutdown signal received", "signal", sig.String())
				application.Stop()
				logger.Info("shutdown complete")
				return nil
			}
		case evt := <-watcher.Events():
			logger.Info("config file changed, reloading", "path", evt.ConfigPath)
			if err := reloadOrRebuild(watchCtx, logger, handler, bs, application, cfgPath); err != nil {
				logger.Error("reload failed", "error", err)
			}
		}
	}
}

// reloadOrRebuild loads the new config, validates it, and either hot-reloads
// modules or triggers a full rebuild + re-exec when the plugin list changed.
func reloadOrRebuild(
	ctx context.Context,
	logger *slog.Logger,
	handler *reload.Handler,
	bs *bootstrap.Bootstrapper,
	application *core.App,
	cfgPath string,
) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("validating config: %w", err)
	}

	// Check for plugin-list changes (requires rebuild).
	if bs != nil && pluginsChanged(cfg.Plugins, bs) {
		logger.Info("plugin list changed, triggering rebuild")

		// Rebuild BEFORE stopping so that if the build fails
		// the running application stays up.
		modules := pluginModules(cfg.Plugins)
		newBinary, err := bs.Rebuild(ctx, modules)
		if err != nil {
			return fmt.Errorf("rebuild failed (app still running): %w", err)
		}

		// Build succeeded — now stop and re-exec.
		logger.Info("rebuild succeeded, stopping application for re-exec")
		application.Stop()
		// ReExec replaces the current process — does not return on success.
		return bs.ReExec(newBinary)
	}

	// Normal hot-reload: call Reload on supporting modules.
	return handler.HandleReloadFromConfig(ctx, cfg)
}

func pluginsChanged(plugins []config.PluginEntry, bs *bootstrap.Bootstrapper) bool {
	modules := pluginModules(plugins)
	return bs.NeedsRebuild(modules)
}

func pluginModules(plugins []config.PluginEntry) []string {
	result := make([]string, len(plugins))
	for i, p := range plugins {
		result[i] = p.String()
	}
	return result
}

// ResolveConfigPath searches for a config file in standard locations.
// Search order: $XDG_CONFIG_HOME/sclaw/sclaw.yaml → ~/.config/sclaw/sclaw.yaml → ./sclaw.yaml
func ResolveConfigPath() (string, error) {
	var candidates []string

	if xdg, ok := os.LookupEnv("XDG_CONFIG_HOME"); ok {
		candidates = append(candidates, filepath.Join(xdg, "sclaw", "sclaw.yaml"))
	} else if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "sclaw", "sclaw.yaml"))
	}

	candidates = append(candidates, "sclaw.yaml")

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("no configuration file found (searched: %v)", candidates)
}

// DefaultDataDir returns the default persistent data directory.
// Uses $XDG_DATA_HOME/sclaw if set, otherwise ~/.local/share/sclaw per the XDG spec.
func DefaultDataDir() string {
	if dir, ok := os.LookupEnv("XDG_DATA_HOME"); ok {
		return filepath.Join(dir, "sclaw")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "sclaw")
}

// DefaultWorkspace returns the current working directory.
func DefaultWorkspace() string {
	dir, _ := os.Getwd()
	return dir
}
