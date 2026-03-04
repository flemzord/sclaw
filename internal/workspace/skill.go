package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// BuiltinPathPrefix is the synthetic path prefix used for skills loaded
// from an embedded filesystem. Skills with this prefix have their body
// inlined in the prompt instead of being loaded via read_file.
const BuiltinPathPrefix = "builtin://"

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

// LoadSkillsFromDir loads skill files from the given directory.
// It reads .md files at the root and also scans one level of subdirectories
// for .md files (e.g. skills/my-skill/SKILL.md).
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
		if entry.IsDir() {
			// Scan one level of subdirectories for .md files.
			subSkills := loadSkillsFromSubdir(filepath.Join(dir, entry.Name()))
			skills = append(skills, subSkills...)
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".md") {
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

// loadSkillsFromSubdir loads .md files from a single subdirectory (non-recursive).
func loadSkillsFromSubdir(dir string) []Skill {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
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
	return skills
}

// ExcludeByName filters out skills whose Meta.Name is in the exclusion set.
// Returns a new slice; the original is not modified.
func ExcludeByName(skills []Skill, names []string) []Skill {
	if len(names) == 0 {
		return skills
	}
	exclude := makeStringSet(names)
	var result []Skill
	for i := range skills {
		if !exclude[skills[i].Meta.Name] {
			result = append(result, skills[i])
		}
	}
	return result
}

// LoadSkillsFromFS loads skill files from an fs.FS (typically an embed.FS).
// It mirrors LoadSkillsFromDir but reads from the virtual filesystem.
// pathPrefix is prepended to each skill's Path for diagnostics (e.g. "builtin://").
// Non-.md files and unparseable files are silently skipped.
// Returns nil without error if fsys is nil.
func LoadSkillsFromFS(fsys fs.FS, pathPrefix string) ([]Skill, error) {
	if fsys == nil {
		return nil, nil
	}

	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("skill: reading embedded FS root: %w", err)
	}

	skills := make([]Skill, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			sub := loadSkillsFromSubFS(fsys, entry.Name(), pathPrefix)
			skills = append(skills, sub...)
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		data, err := fs.ReadFile(fsys, entry.Name())
		if err != nil {
			continue
		}

		skill, err := ParseSkill(string(data), pathPrefix+entry.Name())
		if err != nil {
			continue
		}

		skills = append(skills, skill)
	}

	return skills, nil
}

// loadSkillsFromSubFS loads .md files from a subdirectory within an fs.FS.
func loadSkillsFromSubFS(fsys fs.FS, dir, pathPrefix string) []Skill {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil
	}

	skills := make([]Skill, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := dir + "/" + entry.Name()
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			continue
		}

		skill, err := ParseSkill(string(data), pathPrefix+path)
		if err != nil {
			continue
		}

		skills = append(skills, skill)
	}
	return skills
}

// MergeSkills merges multiple skill slices. Later layers override earlier
// ones by skill name (Meta.Name). The order within each layer is preserved;
// overridden skills keep the position of the first occurrence.
func MergeSkills(layers ...[]Skill) []Skill {
	seen := make(map[string]int) // name → index in result
	var result []Skill

	for _, layer := range layers {
		for _, skill := range layer {
			if idx, ok := seen[skill.Meta.Name]; ok {
				// Override: replace in-place to keep position.
				result[idx] = skill
			} else {
				seen[skill.Meta.Name] = len(result)
				result = append(result, skill)
			}
		}
	}

	return result
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
