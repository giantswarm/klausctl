// Package config defines the klausctl configuration types and handles loading
// from the user's config file.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the klausctl configuration file at ~/.config/klausctl/config.yaml.
// The structure intentionally mirrors the Helm chart values so that knowledge transfers
// between local, standalone, and operator-managed modes.
type Config struct {
	// Runtime is the container runtime: "docker" or "podman".
	// Auto-detected if empty.
	Runtime string `yaml:"runtime,omitempty"`

	// Image is the klaus container image reference.
	Image string `yaml:"image"`

	// Workspace is the host directory to mount into the container at /workspace.
	Workspace string `yaml:"workspace"`

	// Port is the host port mapped to the container's MCP endpoint (8080).
	Port int `yaml:"port"`

	// Claude contains Claude Code agent configuration.
	Claude ClaudeConfig `yaml:"claude,omitempty"`

	// Skills defines inline skills rendered as SKILL.md files.
	Skills map[string]Skill `yaml:"skills,omitempty"`

	// AgentFiles defines markdown-format subagent definitions rendered as .claude/agents/<name>.md.
	AgentFiles map[string]AgentFile `yaml:"agentFiles,omitempty"`

	// Agents defines JSON-format subagents passed via CLAUDE_AGENTS env var (highest priority).
	Agents map[string]AgentConfig `yaml:"agents,omitempty"`

	// Hooks defines lifecycle hooks rendered to settings.json.
	Hooks map[string][]HookMatcher `yaml:"hooks,omitempty"`

	// HookScripts defines hook script contents mounted at /etc/klaus/hooks/<name>.
	HookScripts map[string]string `yaml:"hookScripts,omitempty"`

	// McpServers defines MCP server entries rendered to .mcp.json format.
	McpServers map[string]any `yaml:"mcpServers,omitempty"`

	// Plugins references OCI plugins pulled before container start.
	Plugins []Plugin `yaml:"plugins,omitempty"`

	// EnvForward lists host environment variable names to forward to the container.
	// ANTHROPIC_API_KEY is always forwarded if set.
	EnvForward []string `yaml:"envForward,omitempty"`

	// EnvVars sets explicit environment variable key-value pairs in the container.
	EnvVars map[string]string `yaml:"envVars,omitempty"`
}

// ClaudeConfig contains Claude Code agent configuration, mirroring the Helm values.claude section.
type ClaudeConfig struct {
	// Model is the Claude model (e.g. "sonnet", "opus", "claude-sonnet-4-20250514").
	Model string `yaml:"model,omitempty"`
	// SystemPrompt overrides the default system prompt.
	SystemPrompt string `yaml:"systemPrompt,omitempty"`
	// AppendSystemPrompt appends to the default system prompt.
	AppendSystemPrompt string `yaml:"appendSystemPrompt,omitempty"`
	// MaxTurns limits agentic turns per prompt; 0 means unlimited.
	MaxTurns int `yaml:"maxTurns,omitempty"`
	// PermissionMode controls tool permissions: "default", "acceptEdits",
	// "bypassPermissions", "dontAsk", "plan", "delegate".
	PermissionMode string `yaml:"permissionMode,omitempty"`
	// MaxBudgetUSD caps the maximum dollar spend per invocation; 0 means no limit.
	MaxBudgetUSD float64 `yaml:"maxBudgetUsd,omitempty"`
	// Effort controls effort level: "low", "medium", "high".
	Effort string `yaml:"effort,omitempty"`
	// FallbackModel specifies a model to use when the primary is overloaded.
	FallbackModel string `yaml:"fallbackModel,omitempty"`
	// Tools controls the base set of built-in tools.
	Tools []string `yaml:"tools,omitempty"`
	// AllowedTools restricts tool access with patterns.
	AllowedTools []string `yaml:"allowedTools,omitempty"`
	// DisallowedTools blocks specific tools.
	DisallowedTools []string `yaml:"disallowedTools,omitempty"`
	// StrictMcpConfig when true only uses MCP servers from config.
	StrictMcpConfig bool `yaml:"strictMcpConfig,omitempty"`
	// ActiveAgent selects which agent runs as the top-level agent.
	ActiveAgent string `yaml:"activeAgent,omitempty"`
	// PersistentMode enables bidirectional stream-json mode.
	PersistentMode bool `yaml:"persistentMode,omitempty"`
	// NoSessionPersistence disables saving sessions to disk.
	NoSessionPersistence *bool `yaml:"noSessionPersistence,omitempty"`
	// AddDirs are additional directories for skills and agents.
	AddDirs []string `yaml:"addDirs,omitempty"`
	// PluginDirs are directories to load plugins from.
	PluginDirs []string `yaml:"pluginDirs,omitempty"`
}

// Skill defines an inline Claude Code skill rendered as a SKILL.md file.
type Skill struct {
	// Description is a short description of the skill.
	Description string `yaml:"description,omitempty"`
	// Content is the skill's markdown body.
	Content string `yaml:"content"`
	// DisableModelInvocation prevents the model from invoking this skill automatically.
	DisableModelInvocation bool `yaml:"disableModelInvocation,omitempty"`
	// UserInvocable marks the skill as invocable by the user.
	UserInvocable bool `yaml:"userInvocable,omitempty"`
	// AllowedTools restricts which tools the skill can use.
	AllowedTools string `yaml:"allowedTools,omitempty"`
	// Model overrides the model for this skill.
	Model string `yaml:"model,omitempty"`
	// Context provides additional context for the skill.
	Context any `yaml:"context,omitempty"`
	// Agent associates the skill with a specific agent.
	Agent string `yaml:"agent,omitempty"`
	// ArgumentHint provides a hint for the skill's argument.
	ArgumentHint string `yaml:"argumentHint,omitempty"`
}

// AgentFile defines a markdown-format subagent file.
type AgentFile struct {
	// Content is the raw markdown content for the agent file.
	Content string `yaml:"content"`
}

// AgentConfig defines a JSON-format subagent (highest priority).
// This mirrors the klaus AgentConfig type.
type AgentConfig struct {
	Description     string         `yaml:"description" json:"description"`
	Prompt          string         `yaml:"prompt" json:"prompt"`
	Tools           []string       `yaml:"tools,omitempty" json:"tools,omitempty"`
	DisallowedTools []string       `yaml:"disallowedTools,omitempty" json:"disallowedTools,omitempty"`
	Model           string         `yaml:"model,omitempty" json:"model,omitempty"`
	PermissionMode  string         `yaml:"permissionMode,omitempty" json:"permissionMode,omitempty"`
	MaxTurns        int            `yaml:"maxTurns,omitempty" json:"maxTurns,omitempty"`
	Skills          []string       `yaml:"skills,omitempty" json:"skills,omitempty"`
	McpServers      map[string]any `yaml:"mcpServers,omitempty" json:"mcpServers,omitempty"`
	Hooks           map[string]any `yaml:"hooks,omitempty" json:"hooks,omitempty"`
	Memory          string         `yaml:"memory,omitempty" json:"memory,omitempty"`
}

// HookMatcher defines a hook matcher entry for settings.json.
type HookMatcher struct {
	Matcher string `yaml:"matcher" json:"matcher"`
	Hooks   []Hook `yaml:"hooks" json:"hooks"`
}

// Hook defines a single hook action.
type Hook struct {
	Type    string `yaml:"type" json:"type"`
	Command string `yaml:"command" json:"command"`
}

// Plugin references an OCI plugin artifact.
type Plugin struct {
	Repository string `yaml:"repository"`
	Tag        string `yaml:"tag,omitempty"`
	Digest     string `yaml:"digest,omitempty"`
}

// validPermissionModes lists valid permission mode values.
var validPermissionModes = []string{
	"default", "acceptEdits", "bypassPermissions", "dontAsk", "plan", "delegate",
}

// validEffortLevels lists valid effort level values.
var validEffortLevels = []string{"low", "medium", "high"}

// Load reads and parses the configuration file. If path is empty, the default
// path (~/.config/klausctl/config.yaml) is used.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPaths().ConfigFile
	}
	path = ExpandPath(path)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("config file not found: %s\nRun 'klausctl config init' to create one", path)
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// applyDefaults fills in default values for unset fields.
func (c *Config) applyDefaults() {
	if c.Image == "" {
		c.Image = "ghcr.io/giantswarm/klaus:latest"
	}
	if c.Port == 0 {
		c.Port = 8080
	}
	if c.Claude.PermissionMode == "" {
		c.Claude.PermissionMode = "bypassPermissions"
	}
	if c.Claude.NoSessionPersistence == nil {
		t := true
		c.Claude.NoSessionPersistence = &t
	}
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	if c.Workspace == "" {
		return fmt.Errorf("workspace is required")
	}

	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", c.Port)
	}

	if c.Runtime != "" && c.Runtime != "docker" && c.Runtime != "podman" {
		return fmt.Errorf("runtime must be 'docker' or 'podman', got %q", c.Runtime)
	}

	if c.Claude.PermissionMode != "" {
		if err := validateOneOf("permission mode", c.Claude.PermissionMode, validPermissionModes); err != nil {
			return err
		}
	}

	if c.Claude.Effort != "" {
		if err := validateOneOf("effort level", c.Claude.Effort, validEffortLevels); err != nil {
			return err
		}
	}

	if c.Claude.MaxTurns < 0 {
		return fmt.Errorf("maxTurns must be >= 0, got %d", c.Claude.MaxTurns)
	}

	if c.Claude.MaxBudgetUSD < 0 {
		return fmt.Errorf("maxBudgetUsd must be >= 0, got %f", c.Claude.MaxBudgetUSD)
	}

	for _, p := range c.Plugins {
		if p.Repository == "" {
			return fmt.Errorf("plugin repository is required")
		}
		if p.Tag == "" && p.Digest == "" {
			return fmt.Errorf("plugin %s requires either tag or digest", p.Repository)
		}
	}

	return nil
}

// DefaultConfig returns a minimal default configuration.
func DefaultConfig() *Config {
	t := true
	return &Config{
		Image: "ghcr.io/giantswarm/klaus:latest",
		Port:  8080,
		Claude: ClaudeConfig{
			PermissionMode:       "bypassPermissions",
			NoSessionPersistence: &t,
		},
	}
}

// Marshal serializes the config to YAML.
func (c *Config) Marshal() ([]byte, error) {
	return yaml.Marshal(c)
}

func validateOneOf(name, value string, valid []string) error {
	for _, v := range valid {
		if value == v {
			return nil
		}
	}
	return fmt.Errorf("invalid %s %q; valid values: %s", name, value, strings.Join(valid, ", "))
}
