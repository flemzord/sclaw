package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/flemzord/sclaw/internal/bootstrap"
	"github.com/flemzord/sclaw/internal/cert"
)

// Plugin identifies a third-party Go module to include in the build.
type Plugin struct {
	ModulePath string
	Version    string
	Signature  []byte // optional Ed25519 signature for certification
}

// String returns the module@version representation.
func (p Plugin) String() string {
	if p.Version != "" {
		return p.ModulePath + "@" + p.Version
	}
	return p.ModulePath
}

// BuildRequest contains all parameters for building a custom sclaw binary.
type BuildRequest struct {
	Plugins      []Plugin
	OnlyIDs      []string
	OutputPath   string
	GoPath       string
	SclawVersion string // Go module version for sclaw (e.g. "v0.1.0", "latest")

	// CertVerifier is an optional plugin verifier. When non-nil, each plugin
	// must pass signature verification before it is included in the build.
	CertVerifier *cert.Verifier
}

// Build generates and compiles a custom sclaw binary with the given plugins.
func Build(req BuildRequest) error {
	tmpDir, err := os.MkdirTemp("", "xsclaw-build-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Determine which first-party packages to include.
	firstParty := DefaultModules
	if len(req.OnlyIDs) > 0 {
		firstParty = filterModules(DefaultModules, req.OnlyIDs)
	}

	// Verify plugin signatures when a cert verifier is configured.
	if req.CertVerifier != nil {
		for _, p := range req.Plugins {
			if err := req.CertVerifier.Verify(p.ModulePath, p.Signature); err != nil {
				return fmt.Errorf("plugin %s: certification failed: %w", p.ModulePath, err)
			}
		}
	}

	pluginPkgs := make([]string, len(req.Plugins))
	for i, p := range req.Plugins {
		pluginPkgs[i] = p.ModulePath
	}

	// Compute the build hash from all plugin module paths.
	pluginStrings := make([]string, len(req.Plugins))
	for i, p := range req.Plugins {
		pluginStrings[i] = p.String()
	}
	hash := bootstrap.BuildHash(pluginStrings)

	// Generate main.go.
	mainPath := filepath.Join(tmpDir, "main.go")
	f, err := os.Create(mainPath)
	if err != nil {
		return fmt.Errorf("creating main.go: %w", err)
	}
	if err := GenerateMain(f, CodegenParams{
		SclawVersion:   "latest",
		FirstPartyPkgs: firstParty,
		PluginPkgs:     pluginPkgs,
	}); err != nil {
		_ = f.Close()
		return fmt.Errorf("generating main.go: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing main.go: %w", err)
	}

	// Generate go.mod.
	sclawVer := req.SclawVersion
	if sclawVer == "" {
		sclawVer = "latest"
	}
	if err := generateGoMod(tmpDir, req.Plugins, sclawVer); err != nil {
		return fmt.Errorf("generating go.mod: %w", err)
	}

	ctx := context.Background()
	goCmd := req.GoPath

	// go mod tidy.
	tidy := exec.CommandContext(ctx, goCmd, "mod", "tidy")
	tidy.Dir = tmpDir
	tidy.Stdout = os.Stdout
	tidy.Stderr = os.Stderr
	if err := tidy.Run(); err != nil {
		return fmt.Errorf("go mod tidy failed: %w", err)
	}

	// go build.
	outputAbs, err := filepath.Abs(req.OutputPath)
	if err != nil {
		return fmt.Errorf("resolving output path: %w", err)
	}

	ldflags := fmt.Sprintf("-s -w -X main.buildHash=%s", hash)
	build := exec.CommandContext(ctx, goCmd, "build", "-ldflags", ldflags, "-o", outputAbs, ".")
	build.Dir = tmpDir
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("go build failed: %w", err)
	}

	fmt.Printf("Built %s (hash: %s)\n", outputAbs, hash[:12])
	return nil
}

func generateGoMod(dir string, plugins []Plugin, sclawVersion string) error {
	var b strings.Builder
	b.WriteString("module sclaw-custom\n\n")
	b.WriteString("go 1.25.0\n\n")
	b.WriteString("require (\n")
	fmt.Fprintf(&b, "\tgithub.com/flemzord/sclaw %s\n", sclawVersion)
	for _, p := range plugins {
		if p.Version != "" {
			fmt.Fprintf(&b, "\t%s %s\n", p.ModulePath, p.Version)
		}
	}
	b.WriteString(")\n")

	return os.WriteFile(filepath.Join(dir, "go.mod"), []byte(b.String()), 0o644)
}

// parsePlugins converts "module@version" strings into Plugin structs.
func parsePlugins(raw []string) []Plugin {
	plugins := make([]Plugin, len(raw))
	for i, s := range raw {
		if idx := strings.LastIndex(s, "@"); idx > 0 {
			plugins[i] = Plugin{ModulePath: s[:idx], Version: s[idx+1:]}
		} else {
			plugins[i] = Plugin{ModulePath: s}
		}
	}
	return plugins
}

// filterModules returns only modules whose import paths contain one of the
// given IDs. This is a simple contains check to allow --only to work with
// partial module IDs.
func filterModules(all []string, onlyIDs []string) []string {
	var result []string
	for _, mod := range all {
		for _, id := range onlyIDs {
			if strings.Contains(mod, id) {
				result = append(result, mod)
				break
			}
		}
	}
	return result
}
