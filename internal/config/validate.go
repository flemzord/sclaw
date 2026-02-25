package config

import (
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/flemzord/sclaw/internal/core"
)

// Validate checks the structural validity of a Config.
// It verifies the version field, ensures modules are present,
// and checks that all referenced module IDs exist in the registry.
// It also enforces that Configurable modules have a config entry
// and validates plugin and security settings.
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

	// Strict check: registered Configurable modules must have a config entry.
	for _, info := range core.GetModules() {
		mod := info.New()
		if _, ok := mod.(core.Configurable); ok {
			if _, exists := cfg.Modules[string(info.ID)]; !exists {
				errs = append(errs, fmt.Errorf("config: module %q requires configuration but has no entry", info.ID))
			}
		}
	}

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
	Routing  struct {
		Default bool `yaml:"default"`
	} `yaml:"routing"`
}

// validateAgents checks agent-specific constraints:
//   - At most one agent may be marked as default (routing.default: true).
//   - If an agent references a provider, that provider must exist in cfg.Modules.
func validateAgents(cfg *Config) []error {
	var errs []error
	var defaultAgent string

	for name, node := range cfg.Agents {
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
