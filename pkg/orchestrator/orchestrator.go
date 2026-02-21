// Package orchestrator provides shared container lifecycle logic used by both
// the CLI commands and the MCP server tool handlers.
package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	klausoci "github.com/giantswarm/klaus-oci"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/renderer"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

// BuildRunOptions constructs the container runtime options from config.
// This mirrors the Helm deployment.yaml template, producing the same
// env vars and volume mounts. personalityDir is the local path to the
// resolved personality (empty when no personality is configured).
func BuildRunOptions(cfg *config.Config, paths *config.Paths, containerName, image, personalityDir string) (runtime.RunOptions, error) {
	env, err := BuildEnvVars(cfg, paths)
	if err != nil {
		return runtime.RunOptions{}, err
	}

	volumes := BuildVolumes(cfg, paths, env, personalityDir)

	return runtime.RunOptions{
		Name:    containerName,
		Image:   image,
		Detach:  true,
		User:    fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		EnvVars: env,
		Volumes: volumes,
		Ports:   map[int]int{cfg.Port: 8080},
	}, nil
}

// BuildEnvVars constructs all container environment variables from config.
// These mirror the Helm deployment.yaml env section.
func BuildEnvVars(cfg *config.Config, paths *config.Paths) (map[string]string, error) {
	env := make(map[string]string)

	env["PORT"] = "8080"

	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		env["ANTHROPIC_API_KEY"] = key
	}

	for _, name := range cfg.EnvForward {
		if val := os.Getenv(name); val != "" {
			env[name] = val
		}
	}

	for k, v := range cfg.EnvVars {
		env[k] = v
	}

	setClaudeEnvVars(env, &cfg.Claude)

	if len(cfg.Agents) > 0 {
		agentsJSON, err := json.Marshal(cfg.Agents)
		if err != nil {
			return nil, fmt.Errorf("marshaling agents: %w", err)
		}
		env["CLAUDE_AGENTS"] = string(agentsJSON)
	}

	return env, nil
}

func setClaudeEnvVars(env map[string]string, claude *config.ClaudeConfig) {
	setEnvIfNotEmpty(env, "CLAUDE_MODEL", claude.Model)
	setEnvIfNotEmpty(env, "CLAUDE_SYSTEM_PROMPT", claude.SystemPrompt)
	setEnvIfNotEmpty(env, "CLAUDE_APPEND_SYSTEM_PROMPT", claude.AppendSystemPrompt)
	setEnvIfNotEmpty(env, "CLAUDE_PERMISSION_MODE", claude.PermissionMode)
	setEnvIfNotEmpty(env, "CLAUDE_EFFORT", claude.Effort)
	setEnvIfNotEmpty(env, "CLAUDE_FALLBACK_MODEL", claude.FallbackModel)
	setEnvIfNotEmpty(env, "CLAUDE_ACTIVE_AGENT", claude.ActiveAgent)

	if claude.MaxTurns > 0 {
		env["CLAUDE_MAX_TURNS"] = fmt.Sprintf("%d", claude.MaxTurns)
	}
	if claude.MaxBudgetUSD > 0 {
		env["CLAUDE_MAX_BUDGET_USD"] = fmt.Sprintf("%.2f", claude.MaxBudgetUSD)
	}
	if claude.StrictMcpConfig {
		env["CLAUDE_STRICT_MCP_CONFIG"] = "true"
	}
	if claude.McpTimeout > 0 {
		env["MCP_TIMEOUT"] = fmt.Sprintf("%d", claude.McpTimeout)
	}
	if claude.MaxMcpOutputTokens > 0 {
		env["MAX_MCP_OUTPUT_TOKENS"] = fmt.Sprintf("%d", claude.MaxMcpOutputTokens)
	}
	if claude.IncludePartialMessages {
		env["CLAUDE_INCLUDE_PARTIAL_MESSAGES"] = "true"
	}
	setEnvIfNotEmpty(env, "CLAUDE_JSON_SCHEMA", claude.JsonSchema)
	setEnvIfNotEmpty(env, "CLAUDE_SETTING_SOURCES", claude.SettingSources)
	if claude.PersistentMode {
		env["CLAUDE_PERSISTENT_MODE"] = "true"
	}
	if claude.NoSessionPersistence != nil && *claude.NoSessionPersistence {
		env["CLAUDE_NO_SESSION_PERSISTENCE"] = "true"
	}

	if len(claude.Tools) > 0 {
		env["CLAUDE_TOOLS"] = strings.Join(claude.Tools, ",")
	}
	if len(claude.AllowedTools) > 0 {
		env["CLAUDE_ALLOWED_TOOLS"] = strings.Join(claude.AllowedTools, ",")
	}
	if len(claude.DisallowedTools) > 0 {
		env["CLAUDE_DISALLOWED_TOOLS"] = strings.Join(claude.DisallowedTools, ",")
	}
}

// BuildVolumes constructs the container volume mounts and sets related env vars.
// The env map is mutated to add mount-dependent env vars (CLAUDE_WORKSPACE, etc.).
// personalityDir is the local path to the resolved personality (empty when none).
func BuildVolumes(cfg *config.Config, paths *config.Paths, env map[string]string, personalityDir string) []runtime.Volume {
	var vols []runtime.Volume

	workspace := config.ExpandPath(cfg.Workspace)
	vols = append(vols, runtime.Volume{
		HostPath:      workspace,
		ContainerPath: "/workspace",
	})
	env["CLAUDE_WORKSPACE"] = "/workspace"

	if len(cfg.McpServers) > 0 {
		mcpConfigPath := filepath.Join(paths.RenderedDir, "mcp-config.json")
		vols = append(vols, runtime.Volume{
			HostPath:      mcpConfigPath,
			ContainerPath: "/etc/klaus/mcp-config.json",
			ReadOnly:      true,
		})
		env["CLAUDE_MCP_CONFIG"] = "/etc/klaus/mcp-config.json"
	}

	if len(cfg.Hooks) > 0 {
		settingsPath := filepath.Join(paths.RenderedDir, "settings.json")
		vols = append(vols, runtime.Volume{
			HostPath:      settingsPath,
			ContainerPath: "/etc/klaus/settings.json",
			ReadOnly:      true,
		})
		env["CLAUDE_SETTINGS_FILE"] = "/etc/klaus/settings.json"
	} else if cfg.Claude.SettingsFile != "" {
		env["CLAUDE_SETTINGS_FILE"] = cfg.Claude.SettingsFile
	}

	for name := range cfg.HookScripts {
		hostPath := filepath.Join(paths.RenderedDir, "hooks", name)
		vols = append(vols, runtime.Volume{
			HostPath:      hostPath,
			ContainerPath: "/etc/klaus/hooks/" + name,
			ReadOnly:      true,
		})
	}

	if renderer.HasExtensions(cfg) {
		vols = append(vols, runtime.Volume{
			HostPath:      paths.ExtensionsDir,
			ContainerPath: "/etc/klaus/extensions",
			ReadOnly:      true,
		})
	}

	addDirs := buildAddDirs(cfg)
	if len(addDirs) > 0 {
		env["CLAUDE_ADD_DIRS"] = strings.Join(addDirs, ",")
		if cfg.Claude.LoadAdditionalDirsMemory == nil || *cfg.Claude.LoadAdditionalDirsMemory {
			env["CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD"] = "true"
		}
	}

	if personalityDir != "" && HasSOULFile(personalityDir) {
		soulPath := filepath.Join(personalityDir, "SOUL.md")
		vols = append(vols, runtime.Volume{
			HostPath:      soulPath,
			ContainerPath: "/etc/klaus/SOUL.md",
			ReadOnly:      true,
		})
	}

	pluginDirs := buildPluginDirs(cfg)
	if len(pluginDirs) > 0 {
		env["CLAUDE_PLUGIN_DIRS"] = strings.Join(pluginDirs, ",")
	}

	for _, p := range cfg.Plugins {
		shortName := klausoci.ShortName(p.Repository)
		hostPath := filepath.Join(paths.PluginsDir, shortName)
		vols = append(vols, runtime.Volume{
			HostPath:      hostPath,
			ContainerPath: "/var/lib/klaus/plugins/" + shortName,
			ReadOnly:      true,
		})
	}

	return vols
}

func buildAddDirs(cfg *config.Config) []string {
	var dirs []string
	if renderer.HasExtensions(cfg) {
		dirs = append(dirs, "/etc/klaus/extensions")
	}
	dirs = append(dirs, cfg.Claude.AddDirs...)
	return dirs
}

func buildPluginDirs(cfg *config.Config) []string {
	var dirs []string
	dirs = append(dirs, cfg.Claude.PluginDirs...)
	dirs = append(dirs, PluginDirs(cfg.Plugins)...)
	return dirs
}

func setEnvIfNotEmpty(env map[string]string, key, value string) {
	if value != "" {
		env[key] = value
	}
}
