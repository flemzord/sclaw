// Package main is the entry point for the sclaw CLI.
package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/flemzord/sclaw/internal/config"
	"github.com/flemzord/sclaw/internal/core"
	"github.com/spf13/cobra"
)

// Set by goreleaser ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "sclaw",
		Short:         "A plugin-first, self-hosted personal AI assistant",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(versionCmd(), startCmd(), configCmd())
	return root
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and compiled modules",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("sclaw %s (commit: %s, built: %s)\n", version, commit, date)
			mods := core.GetModules()
			if len(mods) == 0 {
				fmt.Println("\nNo compiled modules.")
				return
			}
			fmt.Println("\nCompiled modules:")
			for _, mod := range mods {
				fmt.Printf("  %s\n", mod.ID)
			}
		},
	}
}

func startCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start sclaw with all configured modules",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")
			if cfgPath == "" {
				resolved, err := resolveConfigPath()
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

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}))

			appCtx := core.NewAppContext(logger, defaultDataDir(), defaultWorkspace())
			appCtx = appCtx.WithModuleConfigs(cfg.Modules)

			app := core.NewApp(appCtx)
			ids := config.Resolve(cfg)
			if err := app.LoadModules(ids); err != nil {
				return err
			}

			return app.Run()
		},
	}
	cmd.Flags().StringP("config", "c", "", "Path to configuration file")
	return cmd
}

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configuration management",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "check <path>",
		Short: "Validate configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := config.Load(args[0])
			if err != nil {
				return err
			}
			if err := config.Validate(cfg); err != nil {
				return err
			}

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}))
			appCtx := core.NewAppContext(logger, defaultDataDir(), defaultWorkspace())
			appCtx = appCtx.WithModuleConfigs(cfg.Modules)

			app := core.NewApp(appCtx)
			ids := config.Resolve(cfg)
			if err := app.LoadModules(ids); err != nil {
				return err
			}
			defer app.Stop()

			fmt.Printf("Configuration OK (%d modules)\n", len(ids))
			for _, id := range ids {
				fmt.Printf("  %s\n", id)
			}
			return nil
		},
	})
	return cmd
}

// resolveConfigPath searches for a config file in standard locations.
// Search order: $XDG_CONFIG_HOME/sclaw/sclaw.yaml → ./sclaw.yaml
func resolveConfigPath() (string, error) {
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

func defaultDataDir() string {
	if dir, ok := os.LookupEnv("XDG_DATA_HOME"); ok {
		return filepath.Join(dir, "sclaw")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "sclaw", "data")
}

func defaultWorkspace() string {
	dir, _ := os.Getwd()
	return dir
}
