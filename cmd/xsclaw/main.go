// Package main is the entry point for the xsclaw build tool.
// xsclaw composes custom sclaw binaries with user-selected modules and plugins,
// similar to how xcaddy works for Caddy.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "xsclaw",
		Short: "Build custom sclaw binaries with selected plugins",
	}
	root.AddCommand(buildCmd())
	return root
}

func buildCmd() *cobra.Command {
	var (
		plugins      []string
		onlyIDs      []string
		output       string
		goPath       string
		sclawVersion string
	)

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build a custom sclaw binary",
		Long: `Build a custom sclaw binary with the specified plugins and modules.

Plugins are Go module paths with optional versions (e.g. github.com/example/plugin@v1.0.0).
The --only flag restricts the build to the specified first-party module IDs.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if output == "" {
				output = "sclaw"
			}
			if goPath == "" {
				goPath = "go"
			}

			req := BuildRequest{
				Plugins:      parsePlugins(plugins),
				OnlyIDs:      onlyIDs,
				OutputPath:   output,
				GoPath:       goPath,
				SclawVersion: sclawVersion,
			}
			return Build(req)
		},
	}

	cmd.Flags().StringSliceVarP(&plugins, "plugin", "p", nil, "Plugin module@version to include (repeatable)")
	cmd.Flags().StringSliceVar(&onlyIDs, "only", nil, "Restrict to these first-party module IDs (repeatable)")
	cmd.Flags().StringVarP(&output, "output", "o", "sclaw", "Output binary path")
	cmd.Flags().StringVar(&goPath, "go", "go", "Path to the go binary")
	cmd.Flags().StringVar(&sclawVersion, "sclaw-version", "latest", "sclaw module version (e.g. v0.1.0)")

	return cmd
}
