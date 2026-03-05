package renderer

import (
	"fmt"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/klausctl/pkg/config"
)

// ContainerClaudeConfig contains the Claude Code settings that the container
// process needs. This is an explicit projection of config.ClaudeConfig that
// excludes host-side orchestration fields (SettingsFile, AddDirs, PluginDirs,
// LoadAdditionalDirsMemory) which are handled by the orchestrator via volume
// mounts and env vars.
type ContainerClaudeConfig struct {
	Model                  string   `yaml:"model,omitempty"`
	SystemPrompt           string   `yaml:"systemPrompt,omitempty"`
	AppendSystemPrompt     string   `yaml:"appendSystemPrompt,omitempty"`
	MaxTurns               int      `yaml:"maxTurns,omitempty"`
	PermissionMode         string   `yaml:"permissionMode,omitempty"`
	MaxBudgetUSD           float64  `yaml:"maxBudgetUsd,omitempty"`
	Effort                 string   `yaml:"effort,omitempty"`
	FallbackModel          string   `yaml:"fallbackModel,omitempty"`
	Tools                  []string `yaml:"tools,omitempty"`
	AllowedTools           []string `yaml:"allowedTools,omitempty"`
	DisallowedTools        []string `yaml:"disallowedTools,omitempty"`
	StrictMcpConfig        bool     `yaml:"strictMcpConfig,omitempty"`
	McpTimeout             int      `yaml:"mcpTimeout,omitempty"`
	MaxMcpOutputTokens     int      `yaml:"maxMcpOutputTokens,omitempty"`
	ActiveAgent            string   `yaml:"activeAgent,omitempty"`
	PersistentMode         bool     `yaml:"persistentMode,omitempty"`
	NoSessionPersistence   *bool    `yaml:"noSessionPersistence,omitempty"`
	IncludePartialMessages bool     `yaml:"includePartialMessages,omitempty"`
	JsonSchema             string   `yaml:"jsonSchema,omitempty"`
	SettingSources         string   `yaml:"settingSources,omitempty"`
}

// ContainerGitConfig contains the git identity settings for the container.
// Host-side fields (CredentialHelper, HTTPSInsteadOfSSH) are excluded because
// the orchestrator handles them via a generated gitconfig volume mount.
type ContainerGitConfig struct {
	AuthorName  string `yaml:"authorName,omitempty"`
	AuthorEmail string `yaml:"authorEmail,omitempty"`
}

// ContainerConfig is the YAML configuration rendered for the klaus container.
// It contains only the fields that the container process needs, replacing 30+
// individual environment variables with a single structured file. Fields that
// drive host-side orchestration (volume mounts, env var assembly) are excluded.
type ContainerConfig struct {
	Workspace string                        `yaml:"workspace"`
	Port      int                           `yaml:"port"`
	Claude    ContainerClaudeConfig         `yaml:"claude,omitempty"`
	Git       ContainerGitConfig            `yaml:"git,omitempty"`
	Agents    map[string]config.AgentConfig `yaml:"agents,omitempty"`
}

// BuildContainerConfig constructs the container-side config from the full
// klausctl Config. Workspace is always "/workspace" and Port is always 8080
// because these are the fixed container-internal values regardless of the
// host-side configuration.
func BuildContainerConfig(cfg *config.Config) *ContainerConfig {
	cc := &ContainerConfig{
		Workspace: "/workspace",
		Port:      8080,
		Claude: ContainerClaudeConfig{
			Model:                  cfg.Claude.Model,
			SystemPrompt:           cfg.Claude.SystemPrompt,
			AppendSystemPrompt:     cfg.Claude.AppendSystemPrompt,
			MaxTurns:               cfg.Claude.MaxTurns,
			PermissionMode:         cfg.Claude.PermissionMode,
			MaxBudgetUSD:           cfg.Claude.MaxBudgetUSD,
			Effort:                 cfg.Claude.Effort,
			FallbackModel:          cfg.Claude.FallbackModel,
			Tools:                  cfg.Claude.Tools,
			AllowedTools:           cfg.Claude.AllowedTools,
			DisallowedTools:        cfg.Claude.DisallowedTools,
			StrictMcpConfig:        cfg.Claude.StrictMcpConfig,
			McpTimeout:             cfg.Claude.McpTimeout,
			MaxMcpOutputTokens:     cfg.Claude.MaxMcpOutputTokens,
			ActiveAgent:            cfg.Claude.ActiveAgent,
			PersistentMode:         cfg.Claude.PersistentMode,
			NoSessionPersistence:   cfg.Claude.NoSessionPersistence,
			IncludePartialMessages: cfg.Claude.IncludePartialMessages,
			JsonSchema:             cfg.Claude.JsonSchema,
			SettingSources:         cfg.Claude.SettingSources,
		},
		Git: ContainerGitConfig{
			AuthorName:  cfg.Git.AuthorName,
			AuthorEmail: cfg.Git.AuthorEmail,
		},
	}

	if len(cfg.Agents) > 0 {
		cc.Agents = cfg.Agents
	}

	return cc
}

// renderContainerConfig writes the container config YAML file to the rendered
// directory. The container reads this file via the KLAUS_CONFIG_FILE env var.
func (r *Renderer) renderContainerConfig(cfg *config.Config) error {
	cc := BuildContainerConfig(cfg)

	data, err := yaml.Marshal(cc)
	if err != nil {
		return fmt.Errorf("marshaling container config: %w", err)
	}

	path := filepath.Join(r.paths.RenderedDir, "config.yaml")
	return writeFile(path, data, 0o600)
}
