package node

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/flemzord/sclaw/internal/core"
	"gopkg.in/yaml.v3"
)

func TestNodeManager_ModuleInfo(t *testing.T) {
	t.Parallel()

	m := &Manager{}
	info := m.ModuleInfo()

	if info.ID != "node.manager" {
		t.Errorf("ID = %q, want %q", info.ID, "node.manager")
	}
	if info.New == nil {
		t.Fatal("New func is nil")
	}

	mod := info.New()
	if _, ok := mod.(*Manager); !ok {
		t.Error("New() should return *Manager")
	}
}

func TestNodeManager_ConfigureDefaults(t *testing.T) {
	t.Parallel()

	m := &Manager{}
	node := mustYAMLNode(t, "{}")

	if err := m.Configure(node); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	if m.config.HeartbeatInterval != "30s" {
		t.Errorf("HeartbeatInterval = %q, want %q", m.config.HeartbeatInterval, "30s")
	}
	if m.config.MaxDevices != 10 {
		t.Errorf("MaxDevices = %d, want %d", m.config.MaxDevices, 10)
	}
	if m.config.ToolTimeout != "30s" {
		t.Errorf("ToolTimeout = %q, want %q", m.config.ToolTimeout, "30s")
	}
}

func TestNodeManager_ConfigureCustom(t *testing.T) {
	t.Parallel()

	m := &Manager{}
	node := mustYAMLNode(t, `
pairing_tokens:
  - "token-1"
  - "token-2"
heartbeat_interval: "15s"
max_devices: 5
tool_timeout: "60s"
`)

	if err := m.Configure(node); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	if len(m.config.PairingTokens) != 2 {
		t.Errorf("PairingTokens = %v, want 2 tokens", m.config.PairingTokens)
	}
	if m.config.HeartbeatInterval != "15s" {
		t.Errorf("HeartbeatInterval = %q, want %q", m.config.HeartbeatInterval, "15s")
	}
	if m.config.MaxDevices != 5 {
		t.Errorf("MaxDevices = %d, want %d", m.config.MaxDevices, 5)
	}
	if m.config.ToolTimeout != "60s" {
		t.Errorf("ToolTimeout = %q, want %q", m.config.ToolTimeout, "60s")
	}
}

func TestNodeManager_ValidateNoPairingTokens(t *testing.T) {
	t.Parallel()

	m := &Manager{}

	node := mustYAMLNode(t, "{}")
	if err := m.Configure(node); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	appCtx := core.NewAppContext(logger, "/data", "/ws")

	if err := m.Provision(appCtx); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if err := m.Validate(); err == nil {
		t.Error("expected validation error for no pairing tokens")
	}
}

func TestNodeManager_ValidateWithTokens(t *testing.T) {
	t.Parallel()

	m := &Manager{}

	node := mustYAMLNode(t, `
pairing_tokens:
  - "secret-token"
`)
	if err := m.Configure(node); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	appCtx := core.NewAppContext(logger, "/data", "/ws")

	if err := m.Provision(appCtx); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if err := m.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}

	if m.heartbeatInterval != 30*time.Second {
		t.Errorf("heartbeatInterval = %v, want 30s", m.heartbeatInterval)
	}
	if m.toolTimeout != 30*time.Second {
		t.Errorf("toolTimeout = %v, want 30s", m.toolTimeout)
	}
}

// mustYAMLNode parses YAML text into a *yaml.Node for Configure calls.
func mustYAMLNode(t *testing.T, text string) *yaml.Node {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(text), &node); err != nil {
		t.Fatalf("YAML parse: %v", err)
	}
	if len(node.Content) > 0 {
		return node.Content[0]
	}
	return &node
}
