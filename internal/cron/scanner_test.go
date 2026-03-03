package cron

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeTestJSON(t *testing.T, dir, name string, def PromptCronDef) {
	t.Helper()
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		t.Fatalf("marshaling test def: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, name+".json"), data)
}

func TestScanPromptCrons_MissingDir(t *testing.T) {
	defs, errs := ScanPromptCrons("/nonexistent/path")
	if defs != nil {
		t.Errorf("expected nil defs for missing dir, got %v", defs)
	}
	if errs != nil {
		t.Errorf("expected nil errs for missing dir, got %v", errs)
	}
}

func TestScanPromptCrons_ValidFiles(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"cron-a", "cron-b"} {
		writeTestJSON(t, dir, name, PromptCronDef{
			Name: name, Schedule: "0 9 * * *", Enabled: true, Prompt: "do something",
		})
	}

	defs, errs := ScanPromptCrons(dir)
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
	if len(defs) != 2 {
		t.Fatalf("expected 2 defs, got %d", len(defs))
	}
}

func TestScanPromptCrons_InvalidFile(t *testing.T) {
	dir := t.TempDir()

	writeTestJSON(t, dir, "valid", PromptCronDef{Name: "valid", Schedule: "* * * * *", Prompt: "ok"})
	writeTestFile(t, filepath.Join(dir, "broken.json"), []byte("{invalid"))

	defs, errs := ScanPromptCrons(dir)
	if len(defs) != 1 {
		t.Errorf("expected 1 valid def, got %d", len(defs))
	}
	if len(errs) != 1 {
		t.Errorf("expected 1 error, got %d", len(errs))
	}
}

func TestScanPromptCrons_IgnoresNonJSON(t *testing.T) {
	dir := t.TempDir()

	writeTestFile(t, filepath.Join(dir, "readme.txt"), []byte("hello"))
	if err := os.MkdirAll(filepath.Join(dir, "results"), 0o755); err != nil {
		t.Fatalf("creating results dir: %v", err)
	}

	writeTestJSON(t, dir, "valid", PromptCronDef{Name: "valid", Schedule: "* * * * *", Prompt: "ok"})

	defs, errs := ScanPromptCrons(dir)
	if len(defs) != 1 {
		t.Errorf("expected 1 def, got %d", len(defs))
	}
	if len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
}
