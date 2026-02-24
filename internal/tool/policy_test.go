package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// stubTool implements Tool for policy testing.
type stubTool struct {
	name          string
	defaultPolicy ApprovalLevel
}

func (s stubTool) Name() string                 { return s.name }
func (s stubTool) Description() string          { return "stub" }
func (s stubTool) Schema() json.RawMessage      { return json.RawMessage(`{}`) }
func (s stubTool) Scopes() []Scope              { return []Scope{ScopeReadOnly} }
func (s stubTool) DefaultPolicy() ApprovalLevel { return s.defaultPolicy }
func (s stubTool) Execute(_ context.Context, _ json.RawMessage, _ ExecutionEnv) (Output, error) {
	return Output{}, nil
}

func TestResolvePolicy_ExplicitToolMapping(t *testing.T) {
	t.Parallel()

	cfg := PolicyConfig{
		DM: Policy{
			Default: ApprovalAsk,
			Tools:   map[string]ApprovalLevel{"read_file": ApprovalAllow},
		},
	}
	tool := stubTool{name: "read_file", defaultPolicy: ApprovalDeny}

	got := ResolvePolicy(cfg, PolicyContextDM, tool)
	if got != ApprovalAllow {
		t.Errorf("explicit mapping: got %q, want %q", got, ApprovalAllow)
	}
}

func TestResolvePolicy_ExplicitToolMapping_TrimmedToolName(t *testing.T) {
	t.Parallel()

	cfg := PolicyConfig{
		DM: Policy{
			Default: ApprovalAllow,
			Tools:   map[string]ApprovalLevel{"read_file": ApprovalDeny},
		},
	}
	tool := stubTool{name: " read_file ", defaultPolicy: ApprovalAllow}

	got := ResolvePolicy(cfg, PolicyContextDM, tool)
	if got != ApprovalDeny {
		t.Errorf("trimmed mapping: got %q, want %q", got, ApprovalDeny)
	}
}

func TestResolvePolicy_ContextDefault(t *testing.T) {
	t.Parallel()

	cfg := PolicyConfig{
		Group: Policy{
			Default: ApprovalDeny,
			Tools:   map[string]ApprovalLevel{},
		},
	}
	tool := stubTool{name: "exec_cmd", defaultPolicy: ApprovalAllow}

	got := ResolvePolicy(cfg, PolicyContextGroup, tool)
	if got != ApprovalDeny {
		t.Errorf("context default: got %q, want %q", got, ApprovalDeny)
	}
}

func TestResolvePolicy_ExplicitListMapping(t *testing.T) {
	t.Parallel()

	cfg := PolicyConfig{
		DM: Policy{
			Default: ApprovalAllow,
			Deny:    []string{"exec_cmd"},
		},
	}
	tool := stubTool{name: "exec_cmd", defaultPolicy: ApprovalAllow}

	got := ResolvePolicy(cfg, PolicyContextDM, tool)
	if got != ApprovalDeny {
		t.Errorf("explicit list mapping: got %q, want %q", got, ApprovalDeny)
	}
}

func TestResolvePolicy_ToolDefault(t *testing.T) {
	t.Parallel()

	cfg := PolicyConfig{
		DM: Policy{}, // No default, no tools mapping
	}
	tool := stubTool{name: "search", defaultPolicy: ApprovalAsk}

	got := ResolvePolicy(cfg, PolicyContextDM, tool)
	if got != ApprovalAsk {
		t.Errorf("tool default: got %q, want %q", got, ApprovalAsk)
	}
}

func TestResolvePolicy_UnknownContext(t *testing.T) {
	t.Parallel()

	cfg := PolicyConfig{
		DM:    Policy{Default: ApprovalDeny},
		Group: Policy{Default: ApprovalDeny},
	}
	tool := stubTool{name: "test", defaultPolicy: ApprovalAllow}

	got := ResolvePolicy(cfg, PolicyContext("unknown"), tool)
	if got != ApprovalAllow {
		t.Errorf("unknown context: got %q, want %q", got, ApprovalAllow)
	}
}

func TestResolvePolicy_ResolutionOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  PolicyConfig
		ctx  PolicyContext
		tool stubTool
		want ApprovalLevel
	}{
		{
			name: "explicit > default > tool",
			cfg: PolicyConfig{
				DM: Policy{
					Default: ApprovalDeny,
					Tools:   map[string]ApprovalLevel{"mytool": ApprovalAllow},
				},
			},
			ctx:  PolicyContextDM,
			tool: stubTool{name: "mytool", defaultPolicy: ApprovalAsk},
			want: ApprovalAllow,
		},
		{
			name: "default > tool when no explicit",
			cfg: PolicyConfig{
				DM: Policy{
					Default: ApprovalDeny,
					Tools:   map[string]ApprovalLevel{},
				},
			},
			ctx:  PolicyContextDM,
			tool: stubTool{name: "mytool", defaultPolicy: ApprovalAllow},
			want: ApprovalDeny,
		},
		{
			name: "tool default when nothing configured",
			cfg:  PolicyConfig{DM: Policy{}},
			ctx:  PolicyContextDM,
			tool: stubTool{name: "mytool", defaultPolicy: ApprovalAsk},
			want: ApprovalAsk,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ResolvePolicy(tt.cfg, tt.ctx, tt.tool)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvePolicy_DMvsGroup(t *testing.T) {
	t.Parallel()

	cfg := PolicyConfig{
		DM:    Policy{Default: ApprovalAllow},
		Group: Policy{Default: ApprovalDeny},
	}
	tool := stubTool{name: "exec", defaultPolicy: ApprovalAsk}

	dmLevel := ResolvePolicy(cfg, PolicyContextDM, tool)
	groupLevel := ResolvePolicy(cfg, PolicyContextGroup, tool)

	if dmLevel != ApprovalAllow {
		t.Errorf("DM: got %q, want %q", dmLevel, ApprovalAllow)
	}
	if groupLevel != ApprovalDeny {
		t.Errorf("Group: got %q, want %q", groupLevel, ApprovalDeny)
	}
}

func TestValidatePolicyConfig_Valid(t *testing.T) {
	t.Parallel()

	cfg := PolicyConfig{
		DM: Policy{
			Default: ApprovalAsk,
			Tools:   map[string]ApprovalLevel{"read": ApprovalAllow, "exec": ApprovalDeny},
		},
		Group: Policy{
			Default: ApprovalDeny,
		},
	}

	if err := ValidatePolicyConfig(cfg); err != nil {
		t.Errorf("valid config should pass: %v", err)
	}
}

func TestValidatePolicyConfig_InvalidDefault(t *testing.T) {
	t.Parallel()

	cfg := PolicyConfig{
		DM: Policy{Default: ApprovalLevel("invalid")},
	}

	if err := ValidatePolicyConfig(cfg); err == nil {
		t.Error("expected error for invalid default level")
	}
}

func TestValidatePolicyConfig_InvalidToolLevel(t *testing.T) {
	t.Parallel()

	cfg := PolicyConfig{
		Group: Policy{
			Tools: map[string]ApprovalLevel{"exec": ApprovalLevel("nope")},
		},
	}

	if err := ValidatePolicyConfig(cfg); err == nil {
		t.Error("expected error for invalid tool level")
	}
}

func TestValidatePolicyConfig_ToolInMultipleLists(t *testing.T) {
	t.Parallel()

	cfg := PolicyConfig{
		DM: Policy{
			Allow: []string{"read_file"},
			Deny:  []string{"read_file"},
		},
	}

	err := ValidatePolicyConfig(cfg)
	if !errors.Is(err, ErrToolInMultipleLists) {
		t.Fatalf("expected ErrToolInMultipleLists, got %v", err)
	}
}

func TestValidatePolicyConfig_ToolInMapAndListConflict(t *testing.T) {
	t.Parallel()

	cfg := PolicyConfig{
		Group: Policy{
			Tools: map[string]ApprovalLevel{"web.search": ApprovalAllow},
			Deny:  []string{"web.search"},
		},
	}

	err := ValidatePolicyConfig(cfg)
	if !errors.Is(err, ErrToolInMultipleLists) {
		t.Fatalf("expected ErrToolInMultipleLists, got %v", err)
	}
}

func TestValidatePolicyConfig_Empty(t *testing.T) {
	t.Parallel()

	if err := ValidatePolicyConfig(PolicyConfig{}); err != nil {
		t.Errorf("empty config should be valid: %v", err)
	}
}
