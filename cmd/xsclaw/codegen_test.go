package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestGenerateMain_NoPlugins(t *testing.T) {
	var buf bytes.Buffer
	err := GenerateMain(&buf, CodegenParams{
		SclawVersion: "v0.1.0",
	})
	if err != nil {
		t.Fatalf("GenerateMain error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"github.com/flemzord/sclaw/pkg/app"`) {
		t.Error("missing sclaw/pkg/app import")
	}
	if !strings.Contains(out, "app.Run(app.RunParams{") {
		t.Error("missing app.Run call")
	}
	// Should not have any blank imports.
	if strings.Contains(out, `_ "`) {
		t.Error("unexpected blank import in output without plugins")
	}
}

func TestGenerateMain_WithPlugins(t *testing.T) {
	var buf bytes.Buffer
	err := GenerateMain(&buf, CodegenParams{
		SclawVersion: "v0.1.0",
		PluginPkgs:   []string{"github.com/example/plugin"},
	})
	if err != nil {
		t.Fatalf("GenerateMain error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `_ "github.com/example/plugin"`) {
		t.Errorf("missing plugin import in:\n%s", out)
	}
}

func TestGenerateMain_WithFirstParty(t *testing.T) {
	var buf bytes.Buffer
	err := GenerateMain(&buf, CodegenParams{
		SclawVersion:   "v0.1.0",
		FirstPartyPkgs: []string{"github.com/flemzord/sclaw/internal/channel/telegram"},
	})
	if err != nil {
		t.Fatalf("GenerateMain error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `_ "github.com/flemzord/sclaw/internal/channel/telegram"`) {
		t.Errorf("missing first-party import in:\n%s", out)
	}
}

func TestGenerateMain_WithOnly(t *testing.T) {
	// When --only is used, only the specified first-party modules are included.
	// This is handled by filterModules in build.go, not in codegen itself.
	// Codegen just emits whatever is passed to it.
	var buf bytes.Buffer
	err := GenerateMain(&buf, CodegenParams{
		SclawVersion:   "v0.1.0",
		FirstPartyPkgs: []string{"github.com/flemzord/sclaw/internal/channel/telegram"},
		PluginPkgs:     []string{"github.com/example/custom"},
	})
	if err != nil {
		t.Fatalf("GenerateMain error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "channel/telegram") {
		t.Error("missing filtered first-party module")
	}
	if !strings.Contains(out, "example/custom") {
		t.Error("missing plugin module")
	}
}
