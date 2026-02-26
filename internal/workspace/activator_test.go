package workspace

import "testing"

func alwaysSkill(name string, tools []string) Skill {
	return Skill{
		Meta: SkillMeta{
			Name:          name,
			ToolsRequired: tools,
			Trigger:       TriggerAlways,
		},
		Body: "Always body.",
	}
}

func autoSkill(name string, tools, keywords []string) Skill {
	return Skill{
		Meta: SkillMeta{
			Name:          name,
			ToolsRequired: tools,
			Trigger:       TriggerAuto,
			Keywords:      keywords,
		},
		Body: "Auto body.",
	}
}

func manualSkill(name string, tools []string) Skill {
	return Skill{
		Meta: SkillMeta{
			Name:          name,
			ToolsRequired: tools,
			Trigger:       TriggerManual,
		},
		Body: "Manual body.",
	}
}

func TestActivator_AlwaysSkills(t *testing.T) {
	t.Parallel()

	a := NewSkillActivator()
	result := a.Activate(ActivateRequest{
		Skills:         []Skill{alwaysSkill("sys", []string{"read_file"})},
		UserMessage:    "hello",
		AvailableTools: []string{"read_file", "write_file"},
	})

	if len(result) != 1 {
		t.Fatalf("got %d skills, want 1", len(result))
	}
	if result[0].Meta.Name != "sys" {
		t.Errorf("name = %q, want %q", result[0].Meta.Name, "sys")
	}
}

func TestActivator_AlwaysMissingTools(t *testing.T) {
	t.Parallel()

	a := NewSkillActivator()
	result := a.Activate(ActivateRequest{
		Skills:         []Skill{alwaysSkill("sys", []string{"missing_tool"})},
		UserMessage:    "hello",
		AvailableTools: []string{"read_file"},
	})

	if len(result) != 0 {
		t.Errorf("got %d skills, want 0 (missing required tool)", len(result))
	}
}

func TestActivator_AutoKeywordMatch(t *testing.T) {
	t.Parallel()

	a := NewSkillActivator()
	result := a.Activate(ActivateRequest{
		Skills:         []Skill{autoSkill("review", []string{"read_file"}, []string{"review", "code quality"})},
		UserMessage:    "Please review this PR",
		AvailableTools: []string{"read_file"},
	})

	if len(result) != 1 {
		t.Fatalf("got %d skills, want 1 (keyword match)", len(result))
	}
}

func TestActivator_AutoNoKeywordMatch(t *testing.T) {
	t.Parallel()

	a := NewSkillActivator()
	result := a.Activate(ActivateRequest{
		Skills:         []Skill{autoSkill("review", []string{"read_file"}, []string{"review"})},
		UserMessage:    "Deploy the service",
		AvailableTools: []string{"read_file"},
	})

	if len(result) != 0 {
		t.Errorf("got %d skills, want 0 (no keyword match)", len(result))
	}
}

func TestActivator_AutoCaseInsensitive(t *testing.T) {
	t.Parallel()

	a := NewSkillActivator()
	result := a.Activate(ActivateRequest{
		Skills:         []Skill{autoSkill("review", []string{"read_file"}, []string{"Review"})},
		UserMessage:    "please REVIEW this",
		AvailableTools: []string{"read_file"},
	})

	if len(result) != 1 {
		t.Fatalf("got %d skills, want 1 (case-insensitive match)", len(result))
	}
}

func TestActivator_ManualActive(t *testing.T) {
	t.Parallel()

	a := NewSkillActivator()
	result := a.Activate(ActivateRequest{
		Skills:         []Skill{manualSkill("deploy", []string{"exec"})},
		UserMessage:    "hello",
		AvailableTools: []string{"exec"},
		ManuallyActive: []string{"deploy"},
	})

	if len(result) != 1 {
		t.Fatalf("got %d skills, want 1 (manually activated)", len(result))
	}
}

func TestActivator_ManualNotActive(t *testing.T) {
	t.Parallel()

	a := NewSkillActivator()
	result := a.Activate(ActivateRequest{
		Skills:         []Skill{manualSkill("deploy", []string{"exec"})},
		UserMessage:    "hello",
		AvailableTools: []string{"exec"},
		ManuallyActive: nil,
	})

	if len(result) != 0 {
		t.Errorf("got %d skills, want 0 (manual not activated)", len(result))
	}
}

func TestActivator_NoToolsRequired(t *testing.T) {
	t.Parallel()

	// Skills with empty ToolsRequired are excluded silently.
	skill := Skill{
		Meta: SkillMeta{
			Name:    "empty-tools",
			Trigger: TriggerAlways,
		},
	}

	a := NewSkillActivator()
	result := a.Activate(ActivateRequest{
		Skills:         []Skill{skill},
		UserMessage:    "hello",
		AvailableTools: []string{"read_file"},
	})

	if len(result) != 0 {
		t.Errorf("got %d skills, want 0 (no tools_required)", len(result))
	}
}

func TestActivator_Ordering(t *testing.T) {
	t.Parallel()

	skills := []Skill{
		manualSkill("manual-1", []string{"exec"}),
		autoSkill("auto-1", []string{"read_file"}, []string{"hello"}),
		alwaysSkill("always-1", []string{"read_file"}),
	}

	a := NewSkillActivator()
	result := a.Activate(ActivateRequest{
		Skills:         skills,
		UserMessage:    "hello world",
		AvailableTools: []string{"read_file", "exec"},
		ManuallyActive: []string{"manual-1"},
	})

	if len(result) != 3 {
		t.Fatalf("got %d skills, want 3", len(result))
	}

	// Order: always → auto → manual.
	if result[0].Meta.Name != "always-1" {
		t.Errorf("result[0] = %q, want always-1", result[0].Meta.Name)
	}
	if result[1].Meta.Name != "auto-1" {
		t.Errorf("result[1] = %q, want auto-1", result[1].Meta.Name)
	}
	if result[2].Meta.Name != "manual-1" {
		t.Errorf("result[2] = %q, want manual-1", result[2].Meta.Name)
	}
}

func TestActivator_EmptyMessage(t *testing.T) {
	t.Parallel()

	a := NewSkillActivator()
	result := a.Activate(ActivateRequest{
		Skills:         []Skill{autoSkill("review", []string{"read_file"}, []string{"review"})},
		UserMessage:    "",
		AvailableTools: []string{"read_file"},
	})

	if len(result) != 0 {
		t.Errorf("got %d skills, want 0 (empty message, no keyword match)", len(result))
	}
}
