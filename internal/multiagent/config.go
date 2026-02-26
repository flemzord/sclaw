// Package multiagent implements multi-agent routing and configuration.
package multiagent

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"
)

// AgentConfig holds the configuration for a single agent.
type AgentConfig struct {
	DataDir   string        `yaml:"data_dir"`
	Workspace string        `yaml:"workspace"`
	Provider  string        `yaml:"provider"`
	Tools     []string      `yaml:"tools"`
	Memory    MemoryConfig  `yaml:"memory"`
	Routing   RoutingConfig `yaml:"routing"`
	Loop      LoopOverrides `yaml:"loop"`
}

// MemoryConfig holds per-agent memory settings.
type MemoryConfig struct {
	Enabled *bool `yaml:"enabled"`
}

// IsEnabled returns whether memory is enabled for this agent.
// Defaults to true when not explicitly set.
func (c MemoryConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// RoutingConfig defines the routing rules that determine when an agent handles a message.
type RoutingConfig struct {
	Channels []string `yaml:"channels"`
	Users    []string `yaml:"users"`
	Groups   []string `yaml:"groups"`
	Default  bool     `yaml:"default"`
}

// LoopOverrides allows per-agent overrides of the ReAct loop parameters.
type LoopOverrides struct {
	MaxIterations int    `yaml:"max_iterations"`
	TokenBudget   int    `yaml:"token_budget"`
	Timeout       string `yaml:"timeout"`
	LoopThreshold int    `yaml:"loop_threshold"`
}

// ParseAgents decodes the raw YAML nodes for the "agents:" section into typed configs.
// It also returns the keys in declaration order (YAML map iteration order).
func ParseAgents(nodes map[string]yaml.Node) (map[string]AgentConfig, []string, error) {
	agents := make(map[string]AgentConfig, len(nodes))
	var order []string
	for id, node := range nodes {
		var cfg AgentConfig
		if err := node.Decode(&cfg); err != nil {
			return nil, nil, fmt.Errorf("multiagent: parsing agent %q: %w", id, err)
		}
		agents[id] = cfg
		order = append(order, id)
	}
	// Sort order for deterministic resolution when YAML map order is lost.
	slices.Sort(order)
	return agents, order, nil
}

// ResolveDefaults fills zero-valued fields with computed defaults.
// Must be called after ParseAgents and before NewRegistry.
func ResolveDefaults(agents map[string]AgentConfig, dataDir string) {
	for name, cfg := range agents {
		if cfg.DataDir == "" {
			cfg.DataDir = filepath.Join(dataDir, "agents", name)
		}
		agents[name] = cfg
	}
}

// EnsureDirectories creates the data directory for each agent.
func EnsureDirectories(agents map[string]AgentConfig) error {
	for name, cfg := range agents {
		if cfg.DataDir == "" {
			continue
		}
		if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
			return fmt.Errorf("multiagent: create data dir for agent %q: %w", name, err)
		}
	}
	return nil
}
