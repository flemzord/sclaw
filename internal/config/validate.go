package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/flemzord/sclaw/internal/core"
)

// Validate checks the structural validity of a Config.
// It verifies the version field, ensures modules are present,
// and checks that all referenced module IDs exist in the registry.
// It also validates plugin, security, and agent settings.
// Configurable modules not listed in config are simply not loaded — no error.
func Validate(cfg *Config) error {
	var errs []error

	if cfg.Version == "" {
		errs = append(errs, errors.New("config: version field is required"))
	} else if cfg.Version != "1" {
		errs = append(errs, fmt.Errorf("config: unsupported version %q (supported: \"1\")", cfg.Version))
	}

	if len(cfg.Modules) == 0 {
		errs = append(errs, errors.New("config: at least one module must be configured"))
	}

	for id := range cfg.Modules {
		if _, ok := core.GetModule(id); !ok {
			errs = append(errs, fmt.Errorf("config: unknown module %q", id))
		}
	}

	// NOTE: Configurable modules NOT listed in cfg.Modules are simply not
	// loaded — that is not an error. We only validate what the user chose
	// to include.

	errs = append(errs, validatePlugins(cfg.Plugins)...)
	errs = append(errs, validateSecurity(cfg.Security)...)

	// Agent validation (skip entirely if no agents defined — backward compatible).
	if len(cfg.Agents) > 0 {
		errs = append(errs, validateAgents(cfg)...)
	}

	return errors.Join(errs...)
}

func validatePlugins(plugins []PluginEntry) []error {
	var errs []error
	for i, p := range plugins {
		if p.Module == "" {
			errs = append(errs, fmt.Errorf("config: plugins[%d]: module path is required", i))
		}
	}
	return errs
}

func validateSecurity(sec *SecurityConfig) []error {
	if sec == nil {
		return nil
	}
	var errs []error

	if sec.Plugins.RequireCertified && len(sec.Plugins.TrustedKeys) == 0 {
		errs = append(errs, errors.New("config: security.plugins.require_certified is true but no trusted_keys provided"))
	}

	for i, hexKey := range sec.Plugins.TrustedKeys {
		raw, err := hex.DecodeString(hexKey)
		if err != nil {
			errs = append(errs, fmt.Errorf("config: security.plugins.trusted_keys[%d]: invalid hex: %w", i, err))
			continue
		}
		if len(raw) != 32 { // ed25519.PublicKeySize
			errs = append(errs, fmt.Errorf("config: security.plugins.trusted_keys[%d]: invalid key size: got %d, want 32", i, len(raw)))
		}
	}

	return errs
}

// agentValidation is a minimal struct used to decode agent YAML nodes
// for validation purposes without importing the multiagent package.
type agentValidation struct {
	Provider string `yaml:"provider"`
	DataDir  string `yaml:"data_dir"`
	Memory   struct {
		Enabled *bool `yaml:"enabled"`
	} `yaml:"memory"`
	Routing struct {
		Default bool `yaml:"default"`
	} `yaml:"routing"`
}

// validateAgents checks agent-specific constraints:
//   - At most one agent may be marked as default (routing.default: true).
//   - If an agent references a provider, that provider must exist in cfg.Modules.
func validateAgents(cfg *Config) []error {
	var errs []error
	var defaultAgent string

	// Sort agent names for deterministic error output when iterating.
	names := make([]string, 0, len(cfg.Agents))
	for name := range cfg.Agents {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		node := cfg.Agents[name]
		var av agentValidation
		if err := node.Decode(&av); err != nil {
			errs = append(errs, fmt.Errorf("config: agent %q: failed to decode: %w", name, err))
			continue
		}

		// Check for duplicate default agents.
		if av.Routing.Default {
			if defaultAgent != "" {
				errs = append(errs, fmt.Errorf(
					"config: multiple agents marked as default: %q and %q",
					defaultAgent, name,
				))
			} else {
				defaultAgent = name
			}
		}

		// Check that referenced provider exists in modules.
		if av.Provider != "" {
			if _, exists := cfg.Modules[av.Provider]; !exists {
				errs = append(errs, fmt.Errorf(
					"config: agent %q references unknown provider module %q",
					name, av.Provider,
				))
			}
		}
	}

	return errs
}
