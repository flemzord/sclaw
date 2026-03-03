package workspace

import (
	"strings"

	ctxengine "github.com/flemzord/sclaw/internal/context"
)

// FormatSkillsForPrompt formats active skills into a compact XML catalog
// suitable for inclusion in the system prompt. The agent can load full
// skill content on demand using read_file with the listed path.
// Returns an empty string if no skills are provided.
func FormatSkillsForPrompt(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("<available_skills>")

	for _, skill := range skills {
		b.WriteString("\n  <skill>")
		b.WriteString("\n    <name>")
		b.WriteString(skill.Meta.Name)
		b.WriteString("</name>")
		if skill.Meta.Description != "" {
			b.WriteString("\n    <description>")
			b.WriteString(skill.Meta.Description)
			b.WriteString("</description>")
		}
		if strings.HasPrefix(skill.Path, BuiltinPathPrefix) {
			// Builtin skills: inline the full body since there is no
			// filesystem path the agent can read_file from.
			b.WriteString("\n    <content>")
			b.WriteString(skill.Body)
			b.WriteString("</content>")
		} else if skill.Path != "" {
			b.WriteString("\n    <location>")
			b.WriteString(skill.Path)
			b.WriteString("</location>")
		}
		b.WriteString("\n  </skill>")
	}

	b.WriteString("\n</available_skills>\n\n")
	b.WriteString("The skill catalog above lists all your available skills. ")
	b.WriteString("You can answer questions about which skills you have by reading this catalog directly. ")
	b.WriteString("For skills with a <content> tag, the full skill is already available inline. ")
	b.WriteString("For skills with a <location> tag, load the full content using the read_file tool with the listed path.")

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
