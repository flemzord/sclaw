package reload

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/flemzord/sclaw/internal/config"
	"github.com/flemzord/sclaw/internal/core"
)

// Handler reloads application configuration and notifies modules.
type Handler struct {
	app       *core.App
	logger    *slog.Logger
	dataDir   string
	workspace string
}

// NewHandler creates a reload handler.
func NewHandler(app *core.App, logger *slog.Logger, dataDir, workspace string) *Handler {
	return &Handler{
		app:       app,
		logger:    logger,
		dataDir:   dataDir,
		workspace: workspace,
	}
}

// HandleReload loads a fresh config from disk, validates it, and calls Reload
// on all modules that implement core.Reloader.
func (h *Handler) HandleReload(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	return h.HandleReloadFromConfig(ctx, cfg)
}

// HandleReloadFromConfig validates a pre-loaded config and calls Reload on
// all modules that implement core.Reloader.
func (h *Handler) HandleReloadFromConfig(_ context.Context, cfg *config.Config) error {
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("validating config: %w", err)
	}

	appCtx := core.NewAppContext(h.logger, h.dataDir, h.workspace)
	appCtx = appCtx.WithModuleConfigs(cfg.Modules)

	if err := h.app.ReloadModules(appCtx); err != nil {
		return fmt.Errorf("reloading modules: %w", err)
	}

	h.logger.Info("configuration reloaded successfully")
	return nil
}
