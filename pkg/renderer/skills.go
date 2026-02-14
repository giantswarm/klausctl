package renderer

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/giantswarm/klausctl/pkg/config"

	"gopkg.in/yaml.v3"
)

// renderSkills writes SKILL.md files for each skill.
// Skills are rendered at: <extensions>/.claude/skills/<name>/SKILL.md
// This mirrors the Helm chart's ConfigMap skill rendering with YAML frontmatter.
func (r *Renderer) renderSkills(skills map[string]config.Skill) error {
	for name, skill := range skills {
		content := renderSkillContent(skill)
		path := filepath.Join(r.paths.ExtensionsDir, ".claude", "skills", name, "SKILL.md")
		if err := writeFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("writing skill %q: %w", name, err)
		}
	}
	return nil
}

// renderSkillContent generates the SKILL.md content with YAML frontmatter.
func renderSkillContent(skill config.Skill) string {
	var buf strings.Builder

	buf.WriteString("---\n")

	if skill.Description != "" {
		buf.WriteString(fmt.Sprintf("description: %q\n", skill.Description))
	}
	if skill.DisableModelInvocation {
		buf.WriteString("disableModelInvocation: true\n")
	}
	if skill.UserInvocable {
		buf.WriteString("userInvocable: true\n")
	}
	if skill.AllowedTools != "" {
		buf.WriteString(fmt.Sprintf("allowedTools: %q\n", skill.AllowedTools))
	}
	if skill.Model != "" {
		buf.WriteString(fmt.Sprintf("model: %q\n", skill.Model))
	}
	if skill.Context != nil {
		contextBytes, err := yaml.Marshal(skill.Context)
		if err == nil {
			buf.WriteString("context:\n")
			for _, line := range strings.Split(strings.TrimRight(string(contextBytes), "\n"), "\n") {
				buf.WriteString("  " + line + "\n")
			}
		}
	}
	if skill.Agent != "" {
		buf.WriteString(fmt.Sprintf("agent: %q\n", skill.Agent))
	}
	if skill.ArgumentHint != "" {
		buf.WriteString(fmt.Sprintf("argumentHint: %q\n", skill.ArgumentHint))
	}

	buf.WriteString("---\n")
	buf.WriteString(skill.Content)

	// Ensure trailing newline.
	if !strings.HasSuffix(skill.Content, "\n") {
		buf.WriteString("\n")
	}

	return buf.String()
}
