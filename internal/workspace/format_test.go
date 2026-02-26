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
		},
	}

	result := FormatSkillsForPrompt(skills)

	if !strings.Contains(result, "## Active Skills") {
		t.Error("missing '## Active Skills' header")
	}
	if !strings.Contains(result, "### code-review — Reviews code") {
		t.Error("missing skill header")
	}
	if !strings.Contains(result, "Check for bugs.") {
		t.Error("missing skill body")
	}
}

func TestFormatSkillsForPrompt_MultipleSkills(t *testing.T) {
	t.Parallel()

	skills := []Skill{
		{
			Meta: SkillMeta{Name: "review", Description: "Code review"},
			Body: "Review body.",
		},
		{
			Meta: SkillMeta{Name: "deploy", Description: "Deployment"},
			Body: "Deploy body.",
		},
	}

	result := FormatSkillsForPrompt(skills)

	if !strings.Contains(result, "### review — Code review") {
		t.Error("missing first skill")
	}
	if !strings.Contains(result, "### deploy — Deployment") {
		t.Error("missing second skill")
	}
}

func TestFormatSkillsForPrompt_NoDescription(t *testing.T) {
	t.Parallel()

	skills := []Skill{
		{
			Meta: SkillMeta{Name: "simple"},
			Body: "Just a body.",
		},
	}

	result := FormatSkillsForPrompt(skills)

	if strings.Contains(result, "—") {
		t.Error("should not contain em-dash when description is empty")
	}
	if !strings.Contains(result, "### simple") {
		t.Error("missing skill name header")
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
