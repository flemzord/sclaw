package workspace

import (
	"strings"
	"testing"
)

// testEstimator counts tokens as words (split by whitespace).
type testEstimator struct{}

func (testEstimator) Estimate(text string) int {
	if text == "" {
		return 0
	}
	return len(strings.Fields(text))
}

func TestFormatSkillsForPrompt_Empty(t *testing.T) {
	t.Parallel()

	result := FormatSkillsForPrompt(nil)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestFormatSkillsForPrompt_SingleSkill(t *testing.T) {
	t.Parallel()

	skills := []Skill{
		{
			Meta: SkillMeta{Name: "code-review", Description: "Reviews code"},
			Body: "Check for bugs.",
			Path: "/data/skills/code-review.md",
		},
	}

	result := FormatSkillsForPrompt(skills)

	if !strings.Contains(result, "<available_skills>") {
		t.Error("missing <available_skills> tag")
	}
	if !strings.Contains(result, "<name>code-review</name>") {
		t.Error("missing skill name")
	}
	if !strings.Contains(result, "<description>Reviews code</description>") {
		t.Error("missing skill description")
	}
	if !strings.Contains(result, "<location>/data/skills/code-review.md</location>") {
		t.Error("missing skill location")
	}
	if strings.Contains(result, "Check for bugs.") {
		t.Error("body should not be included in compact format")
	}
	if !strings.Contains(result, "skill catalog above") {
		t.Error("missing catalog self-reference instruction")
	}
	if !strings.Contains(result, "read_file") {
		t.Error("missing read_file instruction")
	}
}

func TestFormatSkillsForPrompt_MultipleSkills(t *testing.T) {
	t.Parallel()

	skills := []Skill{
		{
			Meta: SkillMeta{Name: "review", Description: "Code review"},
			Body: "Review body.",
			Path: "/data/skills/review.md",
		},
		{
			Meta: SkillMeta{Name: "deploy", Description: "Deployment"},
			Body: "Deploy body.",
			Path: "/data/skills/deploy.md",
		},
	}

	result := FormatSkillsForPrompt(skills)

	if !strings.Contains(result, "<name>review</name>") {
		t.Error("missing first skill")
	}
	if !strings.Contains(result, "<name>deploy</name>") {
		t.Error("missing second skill")
	}
	if !strings.Contains(result, "<description>Code review</description>") {
		t.Error("missing first skill description")
	}
	if !strings.Contains(result, "<description>Deployment</description>") {
		t.Error("missing second skill description")
	}
}

func TestFormatSkillsForPrompt_NoDescription(t *testing.T) {
	t.Parallel()

	skills := []Skill{
		{
			Meta: SkillMeta{Name: "simple"},
			Body: "Just a body.",
			Path: "/data/skills/simple.md",
		},
	}

	result := FormatSkillsForPrompt(skills)

	if strings.Contains(result, "<description>") {
		t.Error("should not contain <description> tag when description is empty")
	}
	if !strings.Contains(result, "<name>simple</name>") {
		t.Error("missing skill name")
	}
}

func TestPruneSkillsToFit_UnderBudget(t *testing.T) {
	t.Parallel()

	skills := []Skill{
		{Meta: SkillMeta{Name: "a", Trigger: TriggerAlways}, Body: "Short."},
	}

	result := PruneSkillsToFit(skills, 1000, testEstimator{})
	if len(result) != 1 {
		t.Errorf("got %d skills, want 1 (under budget)", len(result))
	}
}

func TestPruneSkillsToFit_RemovesAutoFirst(t *testing.T) {
	t.Parallel()

	skills := []Skill{
		{Meta: SkillMeta{Name: "core", Trigger: TriggerAlways}, Body: "Core."},
		{Meta: SkillMeta{Name: "helper", Trigger: TriggerManual}, Body: "Helper."},
		{Meta: SkillMeta{Name: "auto1", Trigger: TriggerAuto}, Body: "Auto skill with a long body that takes tokens."},
	}

	// Set a small budget that forces pruning.
	result := PruneSkillsToFit(skills, 10, testEstimator{})

	for _, s := range result {
		if s.Meta.Trigger == TriggerAuto {
			t.Error("auto skill should have been pruned first")
		}
	}
}

func TestPruneSkillsToFit_AlwaysNeverPruned(t *testing.T) {
	t.Parallel()

	skills := []Skill{
		{Meta: SkillMeta{Name: "core", Trigger: TriggerAlways}, Body: "Core system skill."},
		{Meta: SkillMeta{Name: "auto1", Trigger: TriggerAuto}, Body: "Auto."},
		{Meta: SkillMeta{Name: "manual1", Trigger: TriggerManual}, Body: "Manual."},
	}

	// Budget of 0 forces maximum pruning.
	result := PruneSkillsToFit(skills, 0, testEstimator{})

	if len(result) != 1 {
		t.Fatalf("got %d skills, want 1 (only always)", len(result))
	}
	if result[0].Meta.Name != "core" {
		t.Errorf("remaining skill = %q, want %q", result[0].Meta.Name, "core")
	}
}

func TestFormatSkillsForPrompt_BuiltinInline(t *testing.T) {
	t.Parallel()

	skills := []Skill{
		{
			Meta: SkillMeta{Name: "weather", Description: "Get weather info"},
			Body: "Use the weather API to get forecasts.",
			Path: BuiltinPathPrefix + "weather/SKILL.md",
		},
	}

	result := FormatSkillsForPrompt(skills)

	if !strings.Contains(result, "<name>weather</name>") {
		t.Error("missing skill name")
	}
	if !strings.Contains(result, "<content>Use the weather API to get forecasts.</content>") {
		t.Error("missing inline <content> for builtin skill")
	}
	if strings.Contains(result, "<location>"+BuiltinPathPrefix) {
		t.Error("builtin skill should not have <location> tag with builtin path")
	}
	if !strings.Contains(result, "skills with a <content> tag") {
		t.Error("missing updated instruction text for inline skills")
	}
}

func TestFormatSkillsForPrompt_MixedBuiltinAndFilesystem(t *testing.T) {
	t.Parallel()

	skills := []Skill{
		{
			Meta: SkillMeta{Name: "builtin-skill"},
			Body: "Builtin body.",
			Path: BuiltinPathPrefix + "builtin.md",
		},
		{
			Meta: SkillMeta{Name: "fs-skill"},
			Body: "FS body.",
			Path: "/data/skills/fs-skill.md",
		},
	}

	result := FormatSkillsForPrompt(skills)

	if !strings.Contains(result, "<content>Builtin body.</content>") {
		t.Error("builtin skill should have inline <content>")
	}
	if !strings.Contains(result, "<location>/data/skills/fs-skill.md</location>") {
		t.Error("filesystem skill should have <location>")
	}
	if strings.Contains(result, "<content>FS body.</content>") {
		t.Error("filesystem skill should NOT have inline <content>")
	}
}

func TestPruneSkillsToFit_ZeroBudgetNoAlways(t *testing.T) {
	t.Parallel()

	skills := []Skill{
		{Meta: SkillMeta{Name: "auto1", Trigger: TriggerAuto}, Body: "Auto."},
		{Meta: SkillMeta{Name: "manual1", Trigger: TriggerManual}, Body: "Manual."},
	}

	result := PruneSkillsToFit(skills, 0, testEstimator{})
	if len(result) != 0 {
		t.Errorf("got %d skills, want 0 (zero budget, no always skills)", len(result))
	}
}
