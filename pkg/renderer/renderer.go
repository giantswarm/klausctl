// Package renderer generates the configuration files that are mounted into
// the klaus container. It mirrors the Helm chart's ConfigMap rendering:
// SKILL.md files, settings.json, .mcp.json, agent files, and hook scripts.
package renderer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/giantswarm/klausctl/pkg/config"
)

// Renderer generates configuration files for the klaus container.
type Renderer struct {
	paths *config.Paths
}

// New creates a renderer that writes to the given paths.
func New(paths *config.Paths) *Renderer {
	return &Renderer{paths: paths}
}

// Render generates all configuration files from the config.
// It cleans the rendered directory first to ensure a fresh state.
func (r *Renderer) Render(cfg *config.Config) error {
	// Clean and recreate the rendered directory.
	if err := os.RemoveAll(r.paths.RenderedDir); err != nil {
		return fmt.Errorf("cleaning rendered directory: %w", err)
	}
	if err := config.EnsureDir(r.paths.RenderedDir); err != nil {
		return fmt.Errorf("creating rendered directory: %w", err)
	}

	// Render skills.
	if len(cfg.Skills) > 0 {
		if err := r.renderSkills(cfg.Skills); err != nil {
			return fmt.Errorf("rendering skills: %w", err)
		}
	}

	// Render agent files.
	if len(cfg.AgentFiles) > 0 {
		if err := r.renderAgentFiles(cfg.AgentFiles); err != nil {
			return fmt.Errorf("rendering agent files: %w", err)
		}
	}

	// Render MCP config.
	if len(cfg.McpServers) > 0 {
		if err := r.renderMCPConfig(cfg.McpServers); err != nil {
			return fmt.Errorf("rendering MCP config: %w", err)
		}
	}

	// Render settings (hooks).
	if len(cfg.Hooks) > 0 {
		if err := r.renderSettings(cfg.Hooks); err != nil {
			return fmt.Errorf("rendering settings: %w", err)
		}
	}

	// Render hook scripts.
	if len(cfg.HookScripts) > 0 {
		if err := r.renderHookScripts(cfg.HookScripts); err != nil {
			return fmt.Errorf("rendering hook scripts: %w", err)
		}
	}

	return nil
}

// HasExtensions returns true if there are skills or agent files that need
// to be mounted as an extensions directory.
func HasExtensions(cfg *config.Config) bool {
	return len(cfg.Skills) > 0 || len(cfg.AgentFiles) > 0
}

// writeFile writes data to a file, creating parent directories as needed.
func writeFile(path string, data []byte, mode os.FileMode) error {
	if err := config.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}

// ensureTrailingNewline returns s with a trailing newline appended if missing.
func ensureTrailingNewline(s string) string {
	if !strings.HasSuffix(s, "\n") {
		return s + "\n"
	}
	return s
}
