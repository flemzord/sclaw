package config

import (
	"strings"
	"testing"

	"github.com/flemzord/sclaw/internal/core"
	"gopkg.in/yaml.v3"
)

// stubModule is a basic module for testing.
type stubModule struct {
	id string
}

func (m *stubModule) ModuleInfo() core.ModuleInfo {
	return core.ModuleInfo{
		ID:  core.ModuleID(m.id),
		New: func() core.Module { return &stubModule{id: m.id} },
	}
}

// configurableModule implements core.Configurable.
type configurableModule struct {
	stubModule
}

func (m *configurableModule) ModuleInfo() core.ModuleInfo {
	return core.ModuleInfo{
		ID:  core.ModuleID(m.id),
		New: func() core.Module { return &configurableModule{stubModule: stubModule{id: m.id}} },
	}
}

func (m *configurableModule) Configure(_ *yaml.Node) error { return nil }

func registerStub(t *testing.T, id string) {
	t.Helper()
	core.RegisterModule(&stubModule{id: id})
}

func registerConfigurable(t *testing.T, id string) {
	t.Helper()
	core.RegisterModule(&configurableModule{stubModule: stubModule{id: id}})
}

func TestValidate_Valid(t *testing.T) {
	id := t.Name() + ".mod"
	registerStub(t, id)
	cfg := &Config{
		Version: "1",
		Modules: map[string]yaml.Node{id: {}},
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_MissingVersion(t *testing.T) {
	id := t.Name() + ".mod"
	registerStub(t, id)
	cfg := &Config{
		Modules: map[string]yaml.Node{id: {}},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for missing version")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("error should mention version: %v", err)
	}
}

func TestValidate_UnsupportedVersion(t *testing.T) {
	id := t.Name() + ".mod"
	registerStub(t, id)
	cfg := &Config{
		Version: "99",
		Modules: map[string]yaml.Node{id: {}},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for unsupported version")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error should mention unsupported: %v", err)
	}
}

func TestValidate_EmptyModules(t *testing.T) {
	cfg := &Config{
		Version: "1",
		Modules: map[string]yaml.Node{},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for empty modules")
	}
	if !strings.Contains(err.Error(), "at least one") {
		t.Errorf("error should mention at least one module: %v", err)
	}
}

func TestValidate_UnknownModule(t *testing.T) {
	cfg := &Config{
		Version: "1",
		Modules: map[string]yaml.Node{"unknown.mod": {}},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for unknown module")
	}
	if !strings.Contains(err.Error(), "unknown.mod") {
		t.Errorf("error should mention module ID: %v", err)
	}
}

func TestValidate_MultipleUnknown(t *testing.T) {
	cfg := &Config{
		Version: "1",
		Modules: map[string]yaml.Node{
			"bad.one": {},
			"bad.two": {},
		},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for unknown modules")
	}
	if !strings.Contains(err.Error(), "bad.one") || !strings.Contains(err.Error(), "bad.two") {
		t.Errorf("error should mention both modules: %v", err)
	}
}

func TestValidate_ConfigurableModuleMissingConfig(t *testing.T) {
	id := t.Name() + ".config"
	registerConfigurable(t, id)
	cfg := &Config{
		Version: "1",
		Modules: map[string]yaml.Node{id: {}},
	}
	// Should pass — configurable module has an entry.
	if err := Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_ConfigurableModuleNoEntry(t *testing.T) {
	cfgID := t.Name() + ".config"
	stubID := t.Name() + ".other"
	registerConfigurable(t, cfgID)
	registerStub(t, stubID)
	cfg := &Config{
		Version: "1",
		Modules: map[string]yaml.Node{stubID: {}},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for configurable module without config entry")
	}
	if !strings.Contains(err.Error(), cfgID) {
		t.Errorf("error should mention %s: %v", cfgID, err)
	}
	if !strings.Contains(err.Error(), "requires configuration") {
		t.Errorf("error should mention requires configuration: %v", err)
	}
}
