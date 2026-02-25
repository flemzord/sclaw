// Package config handles YAML configuration loading, environment variable
// expansion, and structural validation for sclaw.
package config

import "gopkg.in/yaml.v3"

// Config is the top-level configuration structure.
type Config struct {
	// Version is the config format version. Currently only "1" is supported.
	Version string `yaml:"version"`

	// Modules maps module IDs to their raw YAML configuration.
	// Keys must match registered module IDs (e.g. "channel.telegram").
	Modules map[string]yaml.Node `yaml:"modules"`

	// Plugins lists third-party Go module plugins to compile into the binary.
	// Used by xsclaw for build-time composition and by the bootstrapper
	// for hot plugin reload detection.
	Plugins []PluginEntry `yaml:"plugins,omitempty"`

	// Security holds optional security settings for plugin certification.
	Security *SecurityConfig `yaml:"security,omitempty"`
}

// PluginEntry identifies a third-party Go module to include in the build.
type PluginEntry struct {
	// Module is the Go module path (e.g. "github.com/example/sclaw-plugin").
	Module string `yaml:"module"`

	// Version is the Go module version (e.g. "v1.0.0").
	Version string `yaml:"version"`
}

// SecurityConfig holds security-related settings.
type SecurityConfig struct {
	Plugins PluginSecurityConfig `yaml:"plugins"`
}

// PluginSecurityConfig controls plugin certification requirements.
type PluginSecurityConfig struct {
	// RequireCertified rejects uncertified plugins at build time.
	RequireCertified bool `yaml:"require_certified"`

	// TrustedKeys is a list of hex-encoded Ed25519 public keys
	// that are allowed to sign plugins.
	TrustedKeys []string `yaml:"trusted_keys,omitempty"`
}
