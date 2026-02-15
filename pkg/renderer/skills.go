package renderer

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/klausctl/pkg/config"
)

// skillFrontmatter defines the YAML frontmatter structure for SKILL.md files.
// Using a struct ensures proper YAML escaping of all values.
type skillFrontmatter struct {
	Description            string `yaml:"description,omitempty"`
	DisableModelInvocation bool   `yaml:"disableModelInvocation,omitempty"`
	UserInvocable          bool   `yaml:"userInvocable,omitempty"`
	AllowedTools           string `yaml:"allowedTools,omitempty"`
	Model                  string `yaml:"model,omitempty"`
	Context                any    `yaml:"context,omitempty"`
	Agent                  string `yaml:"agent,omitempty"`
	ArgumentHint           string `yaml:"argumentHint,omitempty"`
}

// renderSkills writes SKILL.md files for each skill.
// Skills are rendered at: <extensions>/.claude/skills/<name>/SKILL.md
// This mirrors the Helm chart's ConfigMap skill rendering with YAML frontmatter.
func (r *Renderer) renderSkills(skills map[string]config.Skill) error {
	// Sort skill names for deterministic output.
	names := make([]string, 0, len(skills))
	for name := range skills {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		skill := skills[name]
		content, err := renderSkillContent(skill)
		if err != nil {
			return fmt.Errorf("rendering skill %q: %w", name, err)
		}
		path := filepath.Join(r.paths.ExtensionsDir, ".claude", "skills", name, "SKILL.md")
		if err := writeFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("writing skill %q: %w", name, err)
		}
	}
	return nil
}

// renderSkillContent generates the SKILL.md content with YAML frontmatter.
func renderSkillContent(skill config.Skill) (string, error) {
	fm := skillFrontmatter{
		Description:            skill.Description,
		DisableModelInvocation: skill.DisableModelInvocation,
		UserInvocable:          skill.UserInvocable,
		AllowedTools:           skill.AllowedTools,
		Model:                  skill.Model,
		Context:                skill.Context,
		Agent:                  skill.Agent,
		ArgumentHint:           skill.ArgumentHint,
	}

	fmBytes, err := yaml.Marshal(fm)
	if err != nil {
		return "", fmt.Errorf("marshaling frontmatter: %w", err)
	}

	var buf strings.Builder
	buf.WriteString("---\n")
	buf.Write(fmBytes)
	buf.WriteString("---\n")
	buf.WriteString(skill.Content)

	// Ensure trailing newline.
	if !strings.HasSuffix(skill.Content, "\n") {
		buf.WriteString("\n")
	}

	return buf.String(), nil
}
