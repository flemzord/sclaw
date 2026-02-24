package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// envPattern matches ${VAR} and ${VAR:-default} expressions.
var envPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-((?:[^}\\]|\\.)*))?\}`)

// Load reads a YAML configuration file, expands environment variables,
// and parses it into a Config struct.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: reading %s: %w", path, err)
	}

	expanded, err := expandEnv(raw)
	if err != nil {
		return nil, fmt.Errorf("config: expanding variables in %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(expanded, &cfg); err != nil {
		return nil, fmt.Errorf("config: parsing %s: %w", path, err)
	}

	return &cfg, nil
}

// expandEnv replaces ${VAR} and ${VAR:-default} patterns in raw YAML bytes.
// Returns an error listing all unresolved variables (no default, no env value).
func expandEnv(raw []byte) ([]byte, error) {
	var errs []error

	result := envPattern.ReplaceAllFunc(raw, func(match []byte) []byte {
		subs := envPattern.FindSubmatch(match)
		name := string(subs[1])
		hasDefault := len(subs) > 2 && subs[2] != nil
		defaultVal := ""
		if hasDefault {
			defaultVal = string(subs[2])
		}

		value, ok := os.LookupEnv(name)
		if ok {
			return []byte(value)
		}

		if hasDefault {
			return []byte(defaultVal)
		}

		errs = append(errs, fmt.Errorf("unresolved variable: %s", name))
		return match
	})

	return result, errors.Join(errs...)
}
