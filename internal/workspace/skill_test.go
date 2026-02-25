package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const validSkillContent = `---
name: code-review
description: Reviews code for quality issues
tools_required: [read_file]
trigger: auto
keywords: [review, code quality]
---
Check the code for common issues.
`

func TestParseSkill_Valid(t *testing.T) {
	t.Parallel()

	skill, err := ParseSkill(validSkillContent, "test.md")
	if err != nil {
		t.Fatalf("ParseSkill() error: %v", err)
	}

	if skill.Meta.Name != "code-review" {
		t.Errorf("Name = %q, want %q", skill.Meta.Name, "code-review")
	}
	if skill.Meta.Description != "Reviews code for quality issues" {
		t.Errorf("Description = %q", skill.Meta.Description)
	}
	if len(skill.Meta.ToolsRequired) != 1 || skill.Meta.ToolsRequired[0] != "read_file" {
		t.Errorf("ToolsRequired = %v, want [read_file]", skill.Meta.ToolsRequired)
	}
	if skill.Meta.Trigger != TriggerAuto {
		t.Errorf("Trigger = %q, want %q", skill.Meta.Trigger, TriggerAuto)
	}
	if len(skill.Meta.Keywords) != 2 {
		t.Errorf("Keywords = %v, want 2 items", skill.Meta.Keywords)
	}
	if skill.Body != "Check the code for common issues." {
		t.Errorf("Body = %q", skill.Body)
	}
	if skill.Path != "test.md" {
		t.Errorf("Path = %q, want %q", skill.Path, "test.md")
	}
}

func TestParseSkill_DefaultTrigger(t *testing.T) {
	t.Parallel()

	content := `---
name: helper
tools_required: [read_file]
---
Body text.
`
	skill, err := ParseSkill(content, "helper.md")
	if err != nil {
		t.Fatalf("ParseSkill() error: %v", err)
	}

	if skill.Meta.Trigger != TriggerManual {
		t.Errorf("default Trigger = %q, want %q", skill.Meta.Trigger, TriggerManual)
	}
}

func TestParseSkill_MissingFrontmatter(t *testing.T) {
	t.Parallel()

	_, err := ParseSkill("No frontmatter here", "bad.md")
	if !errors.Is(err, ErrNoFrontmatter) {
		t.Errorf("error = %v, want %v", err, ErrNoFrontmatter)
	}
}

func TestParseSkill_InvalidTrigger(t *testing.T) {
	t.Parallel()

	content := `---
name: bad-trigger
tools_required: [read_file]
trigger: sometimes
---
Body.
`
	_, err := ParseSkill(content, "bad.md")
	if !errors.Is(err, ErrInvalidTrigger) {
		t.Errorf("error = %v, want %v", err, ErrInvalidTrigger)
	}
}

func TestParseSkill_MissingName(t *testing.T) {
	t.Parallel()

	content := `---
description: No name field
tools_required: [read_file]
---
Body.
`
	_, err := ParseSkill(content, "noname.md")
	if !errors.Is(err, ErrMissingSkillName) {
		t.Errorf("error = %v, want %v", err, ErrMissingSkillName)
	}
}

func TestLoadSkillsFromDir_MixedFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Valid skill file.
	validPath := filepath.Join(dir, "review.md")
	if err := os.WriteFile(validPath, []byte(validSkillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Invalid skill file (no frontmatter) — should be skipped.
	invalidPath := filepath.Join(dir, "broken.md")
	if err := os.WriteFile(invalidPath, []byte("No frontmatter"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Non-markdown file — should be skipped.
	txtPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txtPath, []byte("Not a skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	skills, err := LoadSkillsFromDir(dir)
	if err != nil {
		t.Fatalf("LoadSkillsFromDir() error: %v", err)
	}

	if len(skills) != 1 {
		t.Fatalf("got %d skills, want 1", len(skills))
	}
	if skills[0].Meta.Name != "code-review" {
		t.Errorf("skill name = %q, want %q", skills[0].Meta.Name, "code-review")
	}
}

func TestLoadSkillsFromDir_MissingDir(t *testing.T) {
	t.Parallel()

	skills, err := LoadSkillsFromDir(filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil {
		t.Fatalf("LoadSkillsFromDir() error: %v", err)
	}
	if skills != nil {
		t.Errorf("got %v, want nil", skills)
	}
}
