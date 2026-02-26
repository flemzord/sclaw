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
	// After removing the strict check (which required ALL registered
	// Configurable modules to have config entries), a configurable module
	// that is not listed in config is simply not loaded. No error expected.
	cfgID := t.Name() + ".config"
	stubID := t.Name() + ".other"
	registerConfigurable(t, cfgID)
	registerStub(t, stubID)
	cfg := &Config{
		Version: "1",
		Modules: map[string]yaml.Node{stubID: {}},
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// yamlNode builds a yaml.Node from a raw YAML string for testing.
func yamlNode(t *testing.T, raw string) yaml.Node {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &node); err != nil {
		t.Fatalf("yamlNode: %v", err)
	}
	// yaml.Unmarshal wraps in a document node; return the first content node.
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return *node.Content[0]
	}
	return node
}

func TestValidate_AgentsEmpty(t *testing.T) {
	id := t.Name() + ".mod"
	registerStub(t, id)
	cfg := &Config{
		Version: "1",
		Modules: map[string]yaml.Node{id: {}},
		Agents:  nil,
	}

	if err := Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_AgentsDuplicateDefault(t *testing.T) {
	id := t.Name() + ".mod"
	registerStub(t, id)
	cfg := &Config{
		Version: "1",
		Modules: map[string]yaml.Node{id: {}},
		Agents: map[string]yaml.Node{
			"agent1": yamlNode(t, `
routing:
  default: true
`),
			"agent2": yamlNode(t, `
routing:
  default: true
`),
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for duplicate default agents")
	}
	if !strings.Contains(err.Error(), "multiple agents marked as default") {
		t.Errorf("error should mention multiple agents marked as default: %v", err)
	}
}

func TestValidate_AgentsUnknownProvider(t *testing.T) {
	id := t.Name() + ".mod"
	registerStub(t, id)
	cfg := &Config{
		Version: "1",
		Modules: map[string]yaml.Node{id: {}},
		Agents: map[string]yaml.Node{
			"support": yamlNode(t, `
provider: provider.foo
`),
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for agent referencing unknown provider")
	}
	if !strings.Contains(err.Error(), "support") {
		t.Errorf("error should mention agent name: %v", err)
	}
	if !strings.Contains(err.Error(), "provider.foo") {
		t.Errorf("error should mention provider module: %v", err)
	}
	if !strings.Contains(err.Error(), "unknown provider module") {
		t.Errorf("error should mention unknown provider module: %v", err)
	}
}

func TestValidate_AgentsValid(t *testing.T) {
	modID := t.Name() + ".mod"
	providerID := t.Name() + ".provider"
	registerStub(t, modID)
	registerStub(t, providerID)
	cfg := &Config{
		Version: "1",
		Modules: map[string]yaml.Node{
			modID:      {},
			providerID: {},
		},
		Agents: map[string]yaml.Node{
			"main": yamlNode(t, `
provider: `+providerID+`
routing:
  default: true
`),
			"fallback": yamlNode(t, `
provider: `+providerID+`
`),
		},
	}

	if err := Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_AgentsWithMemoryAndDataDir(t *testing.T) {
	modID := t.Name() + ".mod"
	providerID := t.Name() + ".provider"
	registerStub(t, modID)
	registerStub(t, providerID)
	cfg := &Config{
		Version: "1",
		Modules: map[string]yaml.Node{
			modID:      {},
			providerID: {},
		},
		Agents: map[string]yaml.Node{
			"assistant": yamlNode(t, `
provider: `+providerID+`
data_dir: /custom/assistant
memory:
  enabled: true
routing:
  default: true
`),
			"researcher": yamlNode(t, `
provider: `+providerID+`
memory:
  enabled: false
`),
		},
	}

	if err := Validate(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
