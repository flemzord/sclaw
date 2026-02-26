package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// TriggerMode controls when a skill is activated.
type TriggerMode string

const (
	// TriggerAlways activates the skill on every message.
	TriggerAlways TriggerMode = "always"
	// TriggerAuto activates when keywords match the user message.
	TriggerAuto TriggerMode = "auto"
	// TriggerManual activates only when explicitly requested.
	TriggerManual TriggerMode = "manual"
)

// Sentinel errors for skill parsing.
var (
	ErrNoFrontmatter    = errors.New("skill: missing YAML frontmatter")
	ErrInvalidTrigger   = errors.New("skill: invalid trigger mode")
	ErrMissingSkillName = errors.New("skill: missing required 'name' field")
)

// SkillMeta holds the YAML frontmatter of a SKILL.md file.
type SkillMeta struct {
	Name          string      `yaml:"name"`
	Description   string      `yaml:"description"`
	ToolsRequired []string    `yaml:"tools_required"`
	Trigger       TriggerMode `yaml:"trigger"`
	Keywords      []string    `yaml:"keywords"`
}

// Skill represents a parsed SKILL.md file.
type Skill struct {
	Meta SkillMeta
	Body string // markdown content after frontmatter
	Path string // source file path (for diagnostics)
}

// ParseSkill parses a SKILL.md file content into a Skill.
// The content must start with YAML frontmatter delimited by "---".
func ParseSkill(content, path string) (Skill, error) {
	front, body, err := splitFrontmatter(content)
	if err != nil {
		return Skill{}, err
	}

	var meta SkillMeta
	if err := yaml.Unmarshal([]byte(front), &meta); err != nil {
		return Skill{}, fmt.Errorf("skill: invalid YAML in %s: %w", path, err)
	}

	if meta.Name == "" {
		return Skill{}, fmt.Errorf("%w in %s", ErrMissingSkillName, path)
	}

	// Default trigger to manual (most restrictive).
	if meta.Trigger == "" {
		meta.Trigger = TriggerManual
	}

	if err := validateTrigger(meta.Trigger); err != nil {
		return Skill{}, fmt.Errorf("%w %q in %s", ErrInvalidTrigger, meta.Trigger, path)
	}

	return Skill{
		Meta: meta,
		Body: strings.TrimSpace(body),
		Path: path,
	}, nil
}

// LoadSkillsFromDir loads all .md files from the given directory.
// Returns nil without error if the directory does not exist.
// Unparseable files are skipped silently.
func LoadSkillsFromDir(dir string) ([]Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	skills := make([]Skill, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		skill, err := ParseSkill(string(data), path)
		if err != nil {
			continue
		}

		skills = append(skills, skill)
	}

	return skills, nil
}

// splitFrontmatter splits content into YAML frontmatter and body.
// The content must begin with "---\n" and have a closing "---\n".
func splitFrontmatter(content string) (front, body string, err error) {
	const delimiter = "---"

	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, delimiter) {
		return "", "", ErrNoFrontmatter
	}

	// Skip the opening delimiter.
	rest := content[len(delimiter):]
	if len(rest) == 0 || rest[0] != '\n' {
		return "", "", ErrNoFrontmatter
	}
	rest = rest[1:]

	// Find the closing delimiter.
	idx := strings.Index(rest, "\n"+delimiter)
	if idx < 0 {
		return "", "", ErrNoFrontmatter
	}

	front = rest[:idx]
	body = rest[idx+1+len(delimiter):]

	return front, body, nil
}

// validateTrigger checks that the trigger mode is one of the known values.
func validateTrigger(t TriggerMode) error {
	switch t {
	case TriggerAlways, TriggerAuto, TriggerManual:
		return nil
	default:
		return ErrInvalidTrigger
	}
}
