package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const shutdownTimeout = 30 * time.Second

// App manages the lifecycle of a set of modules.
type App struct {
	ctx     *AppContext
	modules []moduleInstance
	logger  *slog.Logger
}

type moduleInstance struct {
	id      ModuleID
	module  Module
	started bool
}

// NewApp creates a new App with the given context.
func NewApp(ctx *AppContext) *App {
	return &App{
		ctx:    ctx,
		logger: ctx.Logger.With("component", "core"),
	}
}

// LoadModules instantiates, provisions, and validates all modules for the
// given IDs in order. If any step fails, already-loaded modules are cleaned up.
func (a *App) LoadModules(ids []string) error {
	for _, id := range ids {
		mod, err := a.ctx.LoadModule(id)
		if err != nil {
			a.cleanup()
			return fmt.Errorf("loading module %s: %w", id, err)
		}
		info := mod.ModuleInfo()
		a.modules = append(a.modules, moduleInstance{
			id:     info.ID,
			module: mod,
		})
		a.logger.Info("module loaded", "module", string(info.ID))
	}
	return nil
}

// Start starts all loaded modules that implement Starter, in order.
// If any Start() fails, already-started modules are stopped in reverse order.
func (a *App) Start() error {
	for i := range a.modules {
		mi := &a.modules[i]
		s, ok := mi.module.(Starter)
		if !ok {
			continue
		}
		a.logger.Info("starting module", "module", string(mi.id))
		if err := s.Start(); err != nil {
			a.logger.Error("module start failed", "module", string(mi.id), "error", err)
			a.stopModules(i - 1)
			return fmt.Errorf("starting module %s: %w", mi.id, err)
		}
		mi.started = true
	}
	a.logger.Info("all modules started")
	return nil
}

// Stop stops all started modules in reverse order with a timeout.
func (a *App) Stop() {
	a.stopModules(len(a.modules) - 1)
}

func (a *App) stopModules(fromIndex int) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	for i := fromIndex; i >= 0; i-- {
		mi := &a.modules[i]
		if !mi.started {
			continue
		}
		if s, ok := mi.module.(Stopper); ok {
			a.logger.Info("stopping module", "module", string(mi.id))
			if err := s.Stop(ctx); err != nil {
				a.logger.Error("module stop error", "module", string(mi.id), "error", err)
			}
		}
		mi.started = false
	}
}

func (a *App) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	for i := len(a.modules) - 1; i >= 0; i-- {
		mi := &a.modules[i]
		if s, ok := mi.module.(Stopper); ok {
			_ = s.Stop(ctx)
		}
	}
	a.modules = nil
}

// ReloadModules calls Reload on all loaded modules that implement Reloader.
// Returns a joined error if any module fails to reload.
func (a *App) ReloadModules(ctx *AppContext) error {
	var errs []error
	for i := range a.modules {
		mi := &a.modules[i]
		r, ok := mi.module.(Reloader)
		if !ok {
			continue
		}
		moduleCtx := ctx.ForModule(mi.id)
		a.logger.Info("reloading module", "module", string(mi.id))
		if err := r.Reload(moduleCtx); err != nil {
			a.logger.Error("module reload failed", "module", string(mi.id), "error", err)
			errs = append(errs, fmt.Errorf("reloading module %s: %w", mi.id, err))
		}
	}
	return errors.Join(errs...)
}

// Run starts all modules and blocks until a shutdown signal is received.
func (a *App) Run() error {
	if err := a.Start(); err != nil {
		return err
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	a.logger.Info("shutdown signal received", "signal", sig.String())

	a.Stop()
	a.logger.Info("shutdown complete")
	return nil
}
