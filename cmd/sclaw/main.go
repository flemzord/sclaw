// Package main is the entry point for the sclaw CLI.
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/flemzord/sclaw/internal/config"
	"github.com/flemzord/sclaw/internal/core"
	_ "github.com/flemzord/sclaw/internal/gateway"
	_ "github.com/flemzord/sclaw/modules/channel/telegram"
	_ "github.com/flemzord/sclaw/modules/hook/metrics"
	_ "github.com/flemzord/sclaw/modules/hook/tracing"
	_ "github.com/flemzord/sclaw/modules/memory/sqlite"
	_ "github.com/flemzord/sclaw/modules/provider/openai_compatible"
	_ "github.com/flemzord/sclaw/modules/provider/openai_responses"
	_ "github.com/flemzord/sclaw/modules/tool/file_read"
	_ "github.com/flemzord/sclaw/modules/tool/file_write"
	_ "github.com/flemzord/sclaw/modules/tool/shell"
	"github.com/flemzord/sclaw/pkg/app"
	"github.com/spf13/cobra"
)

// Set by goreleaser ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// debugLogLevel returns slog.LevelDebug when the root --debug flag is set.
func debugLogLevel(cmd *cobra.Command) slog.Level {
	debug, _ := cmd.Root().PersistentFlags().GetBool("debug")
	if debug {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
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
	root.PersistentFlags().Bool("debug", false, "Enable debug logging")
	root.AddCommand(versionCmd(), startCmd(), configCmd(), initCmd(), serviceCmd())
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
				LogLevel:   debugLogLevel(cmd),
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
		Use:   "check [path]",
		Short: "Validate configuration",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var cfgPath string
			if len(args) > 0 {
				cfgPath = args[0]
			} else {
				resolved, err := app.ResolveConfigPath()
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
				Level: debugLogLevel(cmd),
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
