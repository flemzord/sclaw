package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
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

func TestLoadSkillsFromDir_Subdirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Skill in a subdirectory (e.g. skills/my-skill/SKILL.md).
	subDir := filepath.Join(dir, "my-skill")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillContent := "---\nname: my-skill\ntrigger: always\n---\nSkill body.\n"
	if err := os.WriteFile(filepath.Join(subDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Root-level skill alongside the subdirectory.
	if err := os.WriteFile(filepath.Join(dir, "root-skill.md"), []byte(validSkillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	skills, err := LoadSkillsFromDir(dir)
	if err != nil {
		t.Fatalf("LoadSkillsFromDir() error: %v", err)
	}

	if len(skills) != 2 {
		t.Fatalf("got %d skills, want 2 (root + subdirectory)", len(skills))
	}

	names := map[string]bool{}
	for _, s := range skills {
		names[s.Meta.Name] = true
	}
	if !names["code-review"] {
		t.Error("missing root-level skill 'code-review'")
	}
	if !names["my-skill"] {
		t.Error("missing subdirectory skill 'my-skill'")
	}
}

func TestExcludeByName_RemovesMatchingSkills(t *testing.T) {
	t.Parallel()

	skills := []Skill{
		{Meta: SkillMeta{Name: "alpha"}},
		{Meta: SkillMeta{Name: "beta"}},
		{Meta: SkillMeta{Name: "gamma"}},
	}

	result := ExcludeByName(skills, []string{"beta"})
	if len(result) != 2 {
		t.Fatalf("got %d skills, want 2", len(result))
	}
	for _, s := range result {
		if s.Meta.Name == "beta" {
			t.Error("beta should have been excluded")
		}
	}
}

func TestExcludeByName_EmptyExcludeList(t *testing.T) {
	t.Parallel()

	skills := []Skill{
		{Meta: SkillMeta{Name: "alpha"}},
		{Meta: SkillMeta{Name: "beta"}},
	}

	result := ExcludeByName(skills, nil)
	if len(result) != len(skills) {
		t.Errorf("got %d skills, want %d (empty exclude list should be no-op)", len(result), len(skills))
	}
}

func TestExcludeByName_NoMatches(t *testing.T) {
	t.Parallel()

	skills := []Skill{
		{Meta: SkillMeta{Name: "alpha"}},
		{Meta: SkillMeta{Name: "beta"}},
	}

	result := ExcludeByName(skills, []string{"nonexistent"})
	if len(result) != 2 {
		t.Errorf("got %d skills, want 2 (no matches should keep all)", len(result))
	}
}

func TestLoadSkillsFromFS_ValidSkill(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"review.md": &fstest.MapFile{Data: []byte(validSkillContent)},
	}

	skills, err := LoadSkillsFromFS(fsys, BuiltinPathPrefix)
	if err != nil {
		t.Fatalf("LoadSkillsFromFS() error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("got %d skills, want 1", len(skills))
	}
	if skills[0].Meta.Name != "code-review" {
		t.Errorf("Name = %q, want %q", skills[0].Meta.Name, "code-review")
	}
	if skills[0].Path != BuiltinPathPrefix+"review.md" {
		t.Errorf("Path = %q, want %q", skills[0].Path, BuiltinPathPrefix+"review.md")
	}
}

func TestLoadSkillsFromFS_Subdirectory(t *testing.T) {
	t.Parallel()

	subSkill := "---\nname: sub-skill\ntrigger: always\n---\nSub body.\n"
	fsys := fstest.MapFS{
		"root.md":           &fstest.MapFile{Data: []byte(validSkillContent)},
		"my-skill/SKILL.md": &fstest.MapFile{Data: []byte(subSkill)},
	}

	skills, err := LoadSkillsFromFS(fsys, BuiltinPathPrefix)
	if err != nil {
		t.Fatalf("LoadSkillsFromFS() error: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("got %d skills, want 2", len(skills))
	}

	names := map[string]bool{}
	for _, s := range skills {
		names[s.Meta.Name] = true
	}
	if !names["code-review"] {
		t.Error("missing root-level skill 'code-review'")
	}
	if !names["sub-skill"] {
		t.Error("missing subdirectory skill 'sub-skill'")
	}
}

func TestLoadSkillsFromFS_SkipsGoFiles(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"embed.go":  &fstest.MapFile{Data: []byte("package skills")},
		"review.md": &fstest.MapFile{Data: []byte(validSkillContent)},
	}

	skills, err := LoadSkillsFromFS(fsys, BuiltinPathPrefix)
	if err != nil {
		t.Fatalf("LoadSkillsFromFS() error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("got %d skills, want 1 (should skip .go files)", len(skills))
	}
	if skills[0].Meta.Name != "code-review" {
		t.Errorf("Name = %q, want %q", skills[0].Meta.Name, "code-review")
	}
}

func TestLoadSkillsFromFS_NilFS(t *testing.T) {
	t.Parallel()

	skills, err := LoadSkillsFromFS(nil, BuiltinPathPrefix)
	if err != nil {
		t.Fatalf("LoadSkillsFromFS(nil) error: %v", err)
	}
	if skills != nil {
		t.Errorf("got %v, want nil for nil FS", skills)
	}
}

func TestMergeSkills_OverrideByName(t *testing.T) {
	t.Parallel()

	builtin := []Skill{
		{Meta: SkillMeta{Name: "weather"}, Body: "builtin weather", Path: BuiltinPathPrefix + "weather.md"},
		{Meta: SkillMeta{Name: "coding"}, Body: "builtin coding", Path: BuiltinPathPrefix + "coding.md"},
	}
	global := []Skill{
		{Meta: SkillMeta{Name: "weather"}, Body: "custom weather", Path: "/data/skills/weather.md"},
	}

	result := MergeSkills(builtin, global)
	if len(result) != 2 {
		t.Fatalf("got %d skills, want 2", len(result))
	}

	// "weather" should be overridden by global.
	if result[0].Body != "custom weather" {
		t.Errorf("weather Body = %q, want %q (global should override builtin)", result[0].Body, "custom weather")
	}
	if result[0].Path != "/data/skills/weather.md" {
		t.Errorf("weather Path = %q, want global path", result[0].Path)
	}

	// "coding" should remain from builtin.
	if result[1].Body != "builtin coding" {
		t.Errorf("coding Body = %q, want %q", result[1].Body, "builtin coding")
	}
}

func TestMergeSkills_PreservesOrder(t *testing.T) {
	t.Parallel()

	layer1 := []Skill{
		{Meta: SkillMeta{Name: "alpha"}},
		{Meta: SkillMeta{Name: "beta"}},
	}
	layer2 := []Skill{
		{Meta: SkillMeta{Name: "gamma"}},
	}

	result := MergeSkills(layer1, layer2)
	if len(result) != 3 {
		t.Fatalf("got %d skills, want 3", len(result))
	}
	want := []string{"alpha", "beta", "gamma"}
	for i, name := range want {
		if result[i].Meta.Name != name {
			t.Errorf("result[%d].Name = %q, want %q", i, result[i].Meta.Name, name)
		}
	}
}

func TestMergeSkills_EmptyLayers(t *testing.T) {
	t.Parallel()

	result := MergeSkills(nil, nil)
	if len(result) != 0 {
		t.Errorf("got %d skills, want 0 for nil layers", len(result))
	}
}
