package workspace

import "strings"

// ActivateRequest groups the parameters for skill activation.
// Uses request struct pattern (>3 parameters).
type ActivateRequest struct {
	Skills         []Skill
	UserMessage    string
	AvailableTools []string
	ManuallyActive []string // skill names explicitly activated by the user
}

// SkillActivator selects which skills to include in the prompt
// based on trigger rules, available tools, and message content.
type SkillActivator struct{}

// NewSkillActivator creates a new SkillActivator.
func NewSkillActivator() *SkillActivator {
	return &SkillActivator{}
}

// Activate returns the skills that should be active for the given request.
//
// Activation logic (3 passes, preserving order):
//  1. Always — trigger: always, with all required tools available.
//  2. Auto   — trigger: auto, with required tools AND keyword match in message.
//  3. Manual — trigger: manual, with required tools AND name in ManuallyActive.
func (a *SkillActivator) Activate(req ActivateRequest) []Skill {
	toolSet := makeStringSet(req.AvailableTools)
	manualSet := makeStringSet(req.ManuallyActive)
	lowerMsg := strings.ToLower(req.UserMessage)

	var result []Skill

	// Pass 1: always.
	for i := range req.Skills {
		if req.Skills[i].Meta.Trigger == TriggerAlways && hasRequiredTools(req.Skills[i], toolSet) {
			result = append(result, req.Skills[i])
		}
	}

	// Pass 2: auto.
	for i := range req.Skills {
		if req.Skills[i].Meta.Trigger == TriggerAuto && hasRequiredTools(req.Skills[i], toolSet) && keywordMatch(req.Skills[i], lowerMsg) {
			result = append(result, req.Skills[i])
		}
	}

	// Pass 3: manual.
	for i := range req.Skills {
		if req.Skills[i].Meta.Trigger == TriggerManual && hasRequiredTools(req.Skills[i], toolSet) && manualSet[req.Skills[i].Meta.Name] {
			result = append(result, req.Skills[i])
		}
	}

	return result
}

// hasRequiredTools checks that all tools required by the skill are available.
// Skills with no required tools are excluded silently.
func hasRequiredTools(skill Skill, available map[string]bool) bool {
	if len(skill.Meta.ToolsRequired) == 0 {
		return false
	}
	for _, tool := range skill.Meta.ToolsRequired {
		if !available[tool] {
			return false
		}
	}
	return true
}

// keywordMatch checks if any of the skill's keywords appear in the lowercased message.
func keywordMatch(skill Skill, lowerMsg string) bool {
	for _, kw := range skill.Meta.Keywords {
		if strings.Contains(lowerMsg, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// makeStringSet creates a set from a string slice for O(1) lookups.
func makeStringSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, item := range items {
		set[item] = true
	}
	return set
}
