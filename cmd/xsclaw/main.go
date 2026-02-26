// Package main is the entry point for the xsclaw build tool.
// xsclaw composes custom sclaw binaries with user-selected modules and plugins,
// similar to how xcaddy works for Caddy.
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"

	"github.com/flemzord/sclaw/internal/cert"
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
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "xsclaw",
		Short: "Build custom sclaw binaries with selected plugins",
	}
	root.AddCommand(buildCmd(), versionCmd())
	return root
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print xsclaw version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("xsclaw %s (commit: %s, built: %s)\n", version, commit, date)
		},
	}
}

func buildCmd() *cobra.Command {
	var (
		plugins          []string
		pluginSigs       []string
		onlyIDs          []string
		output           string
		goPath           string
		sclawVersion     string
		requireCertified bool
		trustedKeys      []string
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

			parsed, err := parsePlugins(plugins)
			if err != nil {
				return fmt.Errorf("parsing plugins: %w", err)
			}

			// Attach hex-decoded signatures to plugins when provided.
			if len(pluginSigs) > 0 {
				if len(pluginSigs) != len(plugins) {
					return fmt.Errorf("--plugin-sig count (%d) must match --plugin count (%d)", len(pluginSigs), len(plugins))
				}
				for i, hexSig := range pluginSigs {
					sig, err := hex.DecodeString(hexSig)
					if err != nil {
						return fmt.Errorf("invalid signature for plugin %s: %w", parsed[i].ModulePath, err)
					}
					parsed[i].Signature = sig
				}
			}

			req := BuildRequest{
				Plugins:      parsed,
				OnlyIDs:      onlyIDs,
				OutputPath:   output,
				GoPath:       goPath,
				SclawVersion: sclawVersion,
			}

			// Wire cert verifier when certification is required.
			if requireCertified {
				v, err := cert.NewVerifier(cert.VerifyConfig{
					RequireCertified: true,
					TrustedKeys:      trustedKeys,
				})
				if err != nil {
					return fmt.Errorf("creating cert verifier: %w", err)
				}
				req.CertVerifier = v
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			return Build(ctx, req)
		},
	}

	cmd.Flags().StringSliceVarP(&plugins, "plugin", "p", nil, "Plugin module@version to include (repeatable)")
	cmd.Flags().StringSliceVar(&pluginSigs, "plugin-sig", nil, "Hex-encoded Ed25519 signature for each plugin (positional, repeatable)")
	cmd.Flags().StringSliceVar(&onlyIDs, "only", nil, "Restrict to these first-party module IDs (repeatable)")
	cmd.Flags().StringVarP(&output, "output", "o", "sclaw", "Output binary path")
	cmd.Flags().StringVar(&goPath, "go", "go", "Path to the go binary")
	cmd.Flags().StringVar(&sclawVersion, "sclaw-version", "latest", "sclaw module version (e.g. v0.1.0)")
	cmd.Flags().BoolVar(&requireCertified, "require-certified", false, "Reject plugins without valid signatures")
	cmd.Flags().StringSliceVar(&trustedKeys, "trusted-key", nil, "Hex-encoded Ed25519 public key (repeatable)")

	return cmd
}
