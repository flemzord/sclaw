package main

import (
	"fmt"
	"log/slog"

	"github.com/flemzord/sclaw/pkg/app"
	"github.com/kardianos/service"
	"github.com/spf13/cobra"
)

func serviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage sclaw as a system service (daemon)",
	}

	var cfgPath string
	var system bool
	cmd.PersistentFlags().StringVarP(&cfgPath, "config", "c", "", "Path to configuration file")
	cmd.PersistentFlags().BoolVar(&system, "system", false, "Install as system-level service (requires root)")

	cmd.AddCommand(
		serviceInstallCmd(&cfgPath, &system),
		serviceUninstallCmd(&cfgPath, &system),
		serviceStartCmd(&cfgPath, &system),
		serviceStopCmd(&cfgPath, &system),
		serviceStatusCmd(&cfgPath, &system),
	)
	return cmd
}

func serviceInstallCmd(cfgPath *string, system *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install sclaw as a system service",
		RunE: func(_ *cobra.Command, _ []string) error {
			s, err := newService(*cfgPath, *system)
			if err != nil {
				return err
			}
			if err := s.Install(); err != nil {
				return fmt.Errorf("failed to install service: %w", err)
			}
			fmt.Println("Service installed successfully.")
			fmt.Println("Start it with: sclaw service start")
			return nil
		},
	}
}

func serviceUninstallCmd(cfgPath *string, system *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall the sclaw system service",
		RunE: func(_ *cobra.Command, _ []string) error {
			s, err := newService(*cfgPath, *system)
			if err != nil {
				return err
			}
			if err := s.Uninstall(); err != nil {
				return fmt.Errorf("failed to uninstall service: %w", err)
			}
			fmt.Println("Service uninstalled successfully.")
			return nil
		},
	}
}

func serviceStartCmd(cfgPath *string, system *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the sclaw system service",
		RunE: func(_ *cobra.Command, _ []string) error {
			s, err := newService(*cfgPath, *system)
			if err != nil {
				return err
			}
			if err := s.Start(); err != nil {
				return fmt.Errorf("failed to start service: %w", err)
			}
			fmt.Println("Service started.")
			return nil
		},
	}
}

func serviceStopCmd(cfgPath *string, system *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the sclaw system service",
		RunE: func(_ *cobra.Command, _ []string) error {
			s, err := newService(*cfgPath, *system)
			if err != nil {
				return err
			}
			if err := s.Stop(); err != nil {
				return fmt.Errorf("failed to stop service: %w", err)
			}
			fmt.Println("Service stopped.")
			return nil
		},
	}
}

func serviceStatusCmd(cfgPath *string, system *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the status of the sclaw system service",
		RunE: func(_ *cobra.Command, _ []string) error {
			s, err := newService(*cfgPath, *system)
			if err != nil {
				return err
			}
			status, err := s.Status()
			if err != nil {
				return fmt.Errorf("failed to get service status: %w", err)
			}
			switch status {
			case service.StatusRunning:
				fmt.Println("Status: running")
			case service.StatusStopped:
				fmt.Println("Status: stopped")
			default:
				fmt.Println("Status: unknown")
			}
			return nil
		},
	}
}

// newService creates a kardianos/service.Service backed by the sclaw Daemon.
func newService(cfgPath string, system bool) (service.Service, error) {
	daemon := app.NewDaemon(app.RunParams{
		ConfigPath: cfgPath,
		Version:    version,
		Commit:     commit,
		Date:       date,
		LogLevel:   slog.LevelInfo,
	})
	svcConfig := app.ServiceConfig(cfgPath, system)
	return service.New(daemon, svcConfig)
}
