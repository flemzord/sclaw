package workspace

import (
	"strings"

	ctxengine "github.com/flemzord/sclaw/internal/context"
)

// FormatSkillsForPrompt formats active skills into a markdown section
// suitable for inclusion in the system prompt.
// Returns an empty string if no skills are provided.
func FormatSkillsForPrompt(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Active Skills")

	for _, skill := range skills {
		b.WriteString("\n\n### ")
		b.WriteString(skill.Meta.Name)
		if skill.Meta.Description != "" {
			b.WriteString(" — ")
			b.WriteString(skill.Meta.Description)
		}
		if skill.Body != "" {
			b.WriteString("\n\n")
			b.WriteString(skill.Body)
		}
	}

	return b.String()
}

// PruneSkillsToFit removes skills until the formatted output fits within
// the given token budget.
//
// Pruning priority:
//  1. Remove auto skills (last to first).
//  2. Remove manual skills (last to first).
//  3. Always skills are never pruned.
func PruneSkillsToFit(skills []Skill, budget int, estimator ctxengine.TokenEstimator) []Skill {
	if budget <= 0 {
		return filterAlways(skills)
	}

	if estimator.Estimate(FormatSkillsForPrompt(skills)) <= budget {
		return skills
	}

	// Work on a copy to avoid mutating the input.
	result := make([]Skill, len(skills))
	copy(result, skills)

	// Phase 1: remove auto skills (last to first).
	result = pruneByTrigger(result, TriggerAuto, budget, estimator)
	if estimator.Estimate(FormatSkillsForPrompt(result)) <= budget {
		return result
	}

	// Phase 2: remove manual skills (last to first).
	result = pruneByTrigger(result, TriggerManual, budget, estimator)

	return result
}

// pruneByTrigger removes skills of the given trigger type from the end
// until the formatted output fits within budget.
func pruneByTrigger(skills []Skill, trigger TriggerMode, budget int, estimator ctxengine.TokenEstimator) []Skill {
	for i := len(skills) - 1; i >= 0; i-- {
		if estimator.Estimate(FormatSkillsForPrompt(skills)) <= budget {
			break
		}
		if skills[i].Meta.Trigger == trigger {
			skills = append(skills[:i], skills[i+1:]...)
		}
	}
	return skills
}

// filterAlways returns only the skills with trigger: always.
func filterAlways(skills []Skill) []Skill {
	var result []Skill
	for i := range skills {
		if skills[i].Meta.Trigger == TriggerAlways {
			result = append(result, skills[i])
		}
	}
	return result
}
