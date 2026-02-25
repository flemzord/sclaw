package main

import "testing"

func TestParsePlugins(t *testing.T) {
	tests := []struct {
		input      string
		wantModule string
		wantVer    string
	}{
		{"github.com/example/plugin@v1.0.0", "github.com/example/plugin", "v1.0.0"},
		{"github.com/example/plugin", "github.com/example/plugin", ""},
		{"github.com/org/repo@v2.3.4-beta", "github.com/org/repo", "v2.3.4-beta"},
	}

	for _, tt := range tests {
		plugins := parsePlugins([]string{tt.input})
		if len(plugins) != 1 {
			t.Fatalf("expected 1 plugin, got %d", len(plugins))
		}
		p := plugins[0]
		if p.ModulePath != tt.wantModule {
			t.Errorf("parsePlugins(%q).ModulePath = %q, want %q", tt.input, p.ModulePath, tt.wantModule)
		}
		if p.Version != tt.wantVer {
			t.Errorf("parsePlugins(%q).Version = %q, want %q", tt.input, p.Version, tt.wantVer)
		}
	}
}

func TestFilterModules(t *testing.T) {
	all := []string{
		"github.com/flemzord/sclaw/internal/channel/telegram",
		"github.com/flemzord/sclaw/internal/channel/discord",
		"github.com/flemzord/sclaw/internal/provider/anthropic",
	}

	got := filterModules(all, []string{"telegram"})
	if len(got) != 1 {
		t.Fatalf("expected 1 module, got %d: %v", len(got), got)
	}
	if got[0] != all[0] {
		t.Errorf("got %q, want %q", got[0], all[0])
	}
}

func TestFilterModules_NoMatch(t *testing.T) {
	all := []string{
		"github.com/flemzord/sclaw/internal/channel/telegram",
	}
	got := filterModules(all, []string{"nonexistent"})
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestPluginString(t *testing.T) {
	p := Plugin{ModulePath: "github.com/a/b", Version: "v1.0.0"}
	if got := p.String(); got != "github.com/a/b@v1.0.0" {
		t.Errorf("got %q, want %q", got, "github.com/a/b@v1.0.0")
	}

	p2 := Plugin{ModulePath: "github.com/a/b"}
	if got := p2.String(); got != "github.com/a/b" {
		t.Errorf("got %q, want %q", got, "github.com/a/b")
	}
}
