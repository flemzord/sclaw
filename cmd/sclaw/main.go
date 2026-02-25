// Package main is the entry point for the sclaw CLI.
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/flemzord/sclaw/internal/config"
	"github.com/flemzord/sclaw/internal/core"
	"github.com/flemzord/sclaw/pkg/app"
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
			return app.Run(app.RunParams{
				ConfigPath: cfgPath,
				Version:    version,
				Commit:     commit,
				Date:       date,
			})
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
			appCtx := core.NewAppContext(logger, app.DefaultDataDir(), app.DefaultWorkspace())
			appCtx = appCtx.WithModuleConfigs(cfg.Modules)

			application := core.NewApp(appCtx)
			ids := config.Resolve(cfg)
			if err := application.LoadModules(ids); err != nil {
				return err
			}
			defer application.Stop()

			fmt.Printf("Configuration OK (%d modules)\n", len(ids))
			for _, id := range ids {
				fmt.Printf("  %s\n", id)
			}
			return nil
		},
	})
	return cmd
}
