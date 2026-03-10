// Package app provides the shared entry point for sclaw and xsclaw binaries.
package app

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/kardianos/service"
)

// Daemon wraps the sclaw application as a system service.
// It implements the service.Interface required by kardianos/service.
type Daemon struct {
	params RunParams
	logger service.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan error
}

// NewDaemon creates a Daemon that will run sclaw with the given params.
func NewDaemon(params RunParams) *Daemon {
	return &Daemon{
		params: params,
	}
}

// Start is called by the service manager to start the daemon.
// It must not block — the application runs in a background goroutine.
func (d *Daemon) Start(s service.Service) error {
	logger, err := s.Logger(nil)
	if err != nil {
		return err
	}
	d.logger = logger

	ctx, cancel := context.WithCancel(context.Background())

	d.mu.Lock()
	d.cancel = cancel
	d.done = make(chan error, 1)
	d.mu.Unlock()

	go func() {
		d.done <- RunWithContext(ctx, d.params)
	}()

	_ = logger.Info("sclaw service started")
	return nil
}

// Stop is called by the service manager to stop the daemon.
func (d *Daemon) Stop(_ service.Service) error {
	d.mu.Lock()
	cancel := d.cancel
	done := d.done
	d.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		if err := <-done; err != nil {
			_ = d.logger.Error(err)
		}
	}

	_ = d.logger.Info("sclaw service stopped")
	return nil
}

// ServiceConfig returns the default service configuration for sclaw.
// When system is false (default), the service is installed as a user-level
// service (LaunchAgent on macOS, user systemd on Linux). When true, it is
// installed as a system-level daemon (LaunchDaemon / system systemd) which
// typically requires root privileges.
// extraEnv is merged into the service environment variables (from config
// service.env section). PATH is always captured from the current process.
func ServiceConfig(cfgPath string, system bool, extraEnv map[string]string) *service.Config {
	args := []string{"start"}
	if cfgPath != "" {
		args = append(args, "--config", cfgPath)
	}
	// Use the config file's directory as WorkingDirectory so that relative
	// paths in the config (e.g. agents/main) resolve correctly. Without this,
	// launchd defaults to / which is read-only on macOS.
	var workDir string
	if cfgPath != "" {
		workDir = filepath.Dir(cfgPath)
	}

	cfg := &service.Config{
		Name:             "sclaw",
		DisplayName:      "sclaw",
		Description:      "A plugin-first, self-hosted personal AI assistant",
		Arguments:        args,
		WorkingDirectory: workDir,
	}

	// Build service environment: start with user-defined vars from config,
	// then ensure PATH is always captured from the current process so that
	// CLI tools in non-standard locations (e.g. Homebrew) remain accessible
	// under launchd/systemd which use a minimal default PATH.
	envVars := make(map[string]string, len(extraEnv)+1)
	for k, v := range extraEnv {
		envVars[k] = v
	}
	if _, ok := envVars["PATH"]; !ok {
		if path, ok := os.LookupEnv("PATH"); ok && path != "" {
			envVars["PATH"] = path
		}
	}
	if len(envVars) > 0 {
		cfg.EnvVars = envVars
	}

	if !system {
		cfg.Option = service.KeyValue{
			"UserService": true,
		}
	}
	return cfg
}
