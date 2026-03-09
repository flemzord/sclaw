// Package mcp provides per-agent MCP (Model Context Protocol) server integration.
// Each agent can define an mcp.json in its DataDir to connect to external MCP
// servers, exposing their tools as native sclaw tools.
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Config represents the top-level mcp.json configuration.
// The format is compatible with Claude Desktop's mcp.json.
type Config struct {
	Servers map[string]ServerConfig `json:"mcpServers"` //nolint:tagliatelle // Claude Desktop format
}

// ServerConfig defines a single MCP server connection.
// Exactly one of Command (stdio transport) or URL (HTTP transport) must be set.
type ServerConfig struct {
	// Command is the executable for stdio transport.
	Command string `json:"command,omitempty"`
	// Args are the arguments passed to the command.
	Args []string `json:"args,omitempty"`
	// Env are additional environment variables for the subprocess.
	Env []string `json:"env,omitempty"`

	// URL is the endpoint for StreamableHTTP transport.
	URL string `json:"url,omitempty"`
	// Headers are HTTP headers sent with every request.
	Headers map[string]string `json:"headers,omitempty"`
}

// IsStdio returns true if this server uses stdio transport.
func (s ServerConfig) IsStdio() bool { return s.Command != "" }

// IsHTTP returns true if this server uses HTTP transport.
func (s ServerConfig) IsHTTP() bool { return s.URL != "" }

// envVarPattern matches ${VAR} and ${VAR:-default} patterns.
var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-((?:[^}\\]|\\.)*))?}`)

// expandEnvVars replaces ${VAR} and ${VAR:-default} in s with environment values.
func expandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		sub := envVarPattern.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		name := sub[1]
		val, ok := os.LookupEnv(name)
		if ok {
			return val
		}
		if len(sub) >= 3 && sub[2] != "" {
			return strings.ReplaceAll(sub[2], `\}`, "}")
		}
		return match
	})
}

// LoadConfig reads and parses mcp.json from the given data directory.
// Returns (nil, nil) if the file does not exist.
// Returns an error if the file exists but is invalid.
func LoadConfig(dataDir string) (*Config, error) {
	path := filepath.Join(dataDir, "mcp.json")

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("mcp: reading config: %w", err)
	}

	// Expand environment variables in raw JSON before parsing.
	expanded := expandEnvVars(string(data))

	var cfg Config
	if err := json.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("mcp: parsing config %s: %w", path, err)
	}

	// Validate each server entry.
	for name, srv := range cfg.Servers {
		hasCommand := srv.Command != ""
		hasURL := srv.URL != ""
		if hasCommand == hasURL {
			return nil, fmt.Errorf("mcp: server %q must have exactly one of 'command' or 'url'", name)
		}
	}

	return &cfg, nil
}
