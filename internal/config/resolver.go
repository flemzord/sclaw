package config

import "slices"

// Resolve returns a sorted list of module IDs from the configuration.
// The deterministic order ensures consistent module loading.
func Resolve(cfg *Config) []string {
	ids := make([]string, 0, len(cfg.Modules))
	for id := range cfg.Modules {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
