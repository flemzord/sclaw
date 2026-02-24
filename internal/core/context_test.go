package core

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAppContext_ForModule(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx := NewAppContext(logger, "/data", "/workspace")
	child := ctx.ForModule("channel.telegram")

	child.Logger.Info("hello")

	if !bytes.Contains(buf.Bytes(), []byte("channel.telegram")) {
		t.Errorf("expected child logger to contain module ID, got: %s", buf.String())
	}
}

func TestAppContext_LoadModule(t *testing.T) {
	t.Cleanup(resetRegistry)

	provisioned := false
	validated := false

	RegisterModule(&trackingModule{
		id:          "test.loadmod",
		onProvision: func() { provisioned = true },
		onValidate:  func() { validated = true },
	})

	ctx := NewAppContext(nil, "/data", "/ws")
	mod, err := ctx.LoadModule("test.loadmod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mod == nil {
		t.Fatal("expected non-nil module")
	}
	if !provisioned {
		t.Error("expected Provision to be called")
	}
	if !validated {
		t.Error("expected Validate to be called")
	}
}

func TestAppContext_LoadModule_UnknownID(t *testing.T) {
	t.Cleanup(resetRegistry)

	ctx := NewAppContext(nil, "/data", "/ws")
	_, err := ctx.LoadModule("does.not.exist")
	if err == nil {
		t.Fatal("expected error for unknown module")
	}
}

func TestAppContext_LoadModule_ProvisionError(t *testing.T) {
	t.Cleanup(resetRegistry)

	RegisterModule(&trackingModule{
		id:           "test.provfail",
		provisionErr: errors.New("provision boom"),
	})

	ctx := NewAppContext(nil, "/data", "/ws")
	_, err := ctx.LoadModule("test.provfail")
	if err == nil {
		t.Fatal("expected error on provision failure")
	}
}

func TestAppContext_LoadModule_ValidateError(t *testing.T) {
	t.Cleanup(resetRegistry)

	RegisterModule(&trackingModule{
		id:          "test.valfail",
		validateErr: errors.New("validate boom"),
	})

	ctx := NewAppContext(nil, "/data", "/ws")
	_, err := ctx.LoadModule("test.valfail")
	if err == nil {
		t.Fatal("expected error on validate failure")
	}
}

// trackingModule is a test helper that tracks lifecycle calls.
type trackingModule struct {
	id           ModuleID
	onProvision  func()
	onValidate   func()
	provisionErr error
	validateErr  error
}

func (m *trackingModule) ModuleInfo() ModuleInfo {
	id := m.id
	return ModuleInfo{
		ID: id,
		New: func() Module {
			return &trackingModule{
				id:           id,
				onProvision:  m.onProvision,
				onValidate:   m.onValidate,
				provisionErr: m.provisionErr,
				validateErr:  m.validateErr,
			}
		},
	}
}

func (m *trackingModule) Provision(_ *AppContext) error {
	if m.onProvision != nil {
		m.onProvision()
	}
	return m.provisionErr
}

func (m *trackingModule) Validate() error {
	if m.onValidate != nil {
		m.onValidate()
	}
	return m.validateErr
}

// configurableMod is a test module that implements Configurable.
type configurableMod struct {
	id          ModuleID
	configured  *bool
	receivedKey *string
	configErr   error
}

func (m *configurableMod) ModuleInfo() ModuleInfo {
	id := m.id
	return ModuleInfo{
		ID: id,
		New: func() Module {
			return &configurableMod{
				id:          id,
				configured:  m.configured,
				receivedKey: m.receivedKey,
				configErr:   m.configErr,
			}
		},
	}
}

func (m *configurableMod) Configure(node *yaml.Node) error {
	if m.configErr != nil {
		return m.configErr
	}
	if m.configured != nil {
		*m.configured = true
	}
	if m.receivedKey != nil {
		var parsed struct {
			Key string `yaml:"key"`
		}
		if err := node.Decode(&parsed); err != nil {
			return err
		}
		*m.receivedKey = parsed.Key
	}
	return nil
}

func TestAppContext_LoadModule_WithConfig(t *testing.T) {
	t.Cleanup(resetRegistry)

	configured := false
	receivedKey := ""
	RegisterModule(&configurableMod{
		id:          "test.cfgmod",
		configured:  &configured,
		receivedKey: &receivedKey,
	})

	// Build a yaml.Node for the module config.
	var node yaml.Node
	if err := yaml.Unmarshal([]byte("key: hello"), &node); err != nil {
		t.Fatal(err)
	}

	ctx := NewAppContext(nil, "/data", "/ws")
	ctx = ctx.WithModuleConfigs(map[string]yaml.Node{
		"test.cfgmod": *node.Content[0],
	})

	mod, err := ctx.LoadModule("test.cfgmod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mod == nil {
		t.Fatal("expected non-nil module")
	}
	if !configured {
		t.Error("expected Configure to be called")
	}
	if receivedKey != "hello" {
		t.Errorf("receivedKey = %q, want %q", receivedKey, "hello")
	}
}

func TestAppContext_LoadModule_ConfigError(t *testing.T) {
	t.Cleanup(resetRegistry)

	RegisterModule(&configurableMod{
		id:        "test.cfgerr",
		configErr: errors.New("config boom"),
	})

	var node yaml.Node
	if err := yaml.Unmarshal([]byte("key: val"), &node); err != nil {
		t.Fatal(err)
	}

	ctx := NewAppContext(nil, "/data", "/ws")
	ctx = ctx.WithModuleConfigs(map[string]yaml.Node{
		"test.cfgerr": *node.Content[0],
	})

	_, err := ctx.LoadModule("test.cfgerr")
	if err == nil {
		t.Fatal("expected error on configure failure")
	}
}

func TestAppContext_LoadModule_NoConfig(t *testing.T) {
	t.Cleanup(resetRegistry)

	configured := false
	RegisterModule(&configurableMod{
		id:         "test.noconfig",
		configured: &configured,
	})

	// No config provided — Configure should not be called.
	ctx := NewAppContext(nil, "/data", "/ws")
	_, err := ctx.LoadModule("test.noconfig")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if configured {
		t.Error("Configure should not be called when no config is provided")
	}
}

func TestAppContext_LoadModule_NotConfigurable(t *testing.T) {
	t.Cleanup(resetRegistry)

	provisioned := false
	RegisterModule(&trackingModule{
		id:          "test.notcfg",
		onProvision: func() { provisioned = true },
	})

	var node yaml.Node
	if err := yaml.Unmarshal([]byte("key: val"), &node); err != nil {
		t.Fatal(err)
	}

	// Config exists but module doesn't implement Configurable — should be ignored.
	ctx := NewAppContext(nil, "/data", "/ws")
	ctx = ctx.WithModuleConfigs(map[string]yaml.Node{
		"test.notcfg": *node.Content[0],
	})

	_, err := ctx.LoadModule("test.notcfg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !provisioned {
		t.Error("expected Provision to be called")
	}
}

func TestAppContext_ForModule_PropagatesConfig(t *testing.T) {
	var node yaml.Node
	if err := yaml.Unmarshal([]byte("key: val"), &node); err != nil {
		t.Fatal(err)
	}

	ctx := NewAppContext(nil, "/data", "/ws")
	ctx = ctx.WithModuleConfigs(map[string]yaml.Node{
		"test.mod": *node.Content[0],
	})

	child := ctx.ForModule("test.mod")
	if child.moduleConfigs == nil {
		t.Fatal("ForModule should propagate moduleConfigs")
	}
	if _, ok := child.moduleConfigs["test.mod"]; !ok {
		t.Error("child context should have test.mod config")
	}
}
