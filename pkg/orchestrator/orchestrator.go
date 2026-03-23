// Package orchestrator provides shared container lifecycle logic used by both
// the CLI commands and the MCP server tool handlers.
package orchestrator

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"

	klausoci "github.com/giantswarm/klaus-oci"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/mcpserverstore"
	"github.com/giantswarm/klausctl/pkg/oauth"
	"github.com/giantswarm/klausctl/pkg/renderer"
	"github.com/giantswarm/klausctl/pkg/runtime"
	"github.com/giantswarm/klausctl/pkg/secret"
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

	volumes, err := BuildVolumes(cfg, paths, env, personalityDir)
	if err != nil {
		return runtime.RunOptions{}, err
	}

	opts := runtime.RunOptions{
		Name:    containerName,
		Image:   image,
		Detach:  true,
		User:    fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		EnvVars: env,
		Volumes: volumes,
		Ports:   map[int]int{cfg.Port: 8080},
	}

	if needsDockerInternalHost(cfg) {
		opts.ExtraHosts = append(opts.ExtraHosts, "host.docker.internal:host-gateway")
	}

	return opts, nil
}

// needsDockerInternalHost reports whether the container needs an explicit
// host.docker.internal mapping. On Linux, Docker does not provide this
// automatically (unlike Docker Desktop on macOS/Windows), so we add
// --add-host host.docker.internal:host-gateway when any resolved MCP server
// URL references host.docker.internal.
func needsDockerInternalHost(cfg *config.Config) bool {
	if goruntime.GOOS != "linux" {
		return false
	}
	for _, v := range cfg.McpServers {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		url, _ := m["url"].(string)
		if strings.Contains(url, "host.docker.internal") {
			return true
		}
	}
	return false
}

// BuildEnvVars constructs all container environment variables from config.
// Claude, Git, and Agent settings are now rendered into the container config
// YAML file (see renderer.BuildContainerConfig). This function only sets
// env vars for secrets, forwarded vars, and the config file path.
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

	if len(cfg.SecretEnvVars) > 0 {
		store, err := secret.Load(paths.SecretsFile)
		if err != nil {
			return nil, fmt.Errorf("loading secrets for env vars: %w", err)
		}
		for envName, secretName := range cfg.SecretEnvVars {
			if err := secret.ValidateName(secretName); err != nil {
				return nil, fmt.Errorf("secretEnvVars[%s]: %w", envName, err)
			}
			val, err := store.Get(secretName)
			if err != nil {
				return nil, fmt.Errorf("resolving secretEnvVars[%s]: %w", envName, err)
			}
			env[envName] = val
		}
	}

	// Git and Claude settings are now in the container config YAML.
	// Keep env vars as fallback for backward compatibility with older
	// container images that don't yet read the config file.
	setGitEnvVars(env, &cfg.Git)
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
	if claude.Mode != "" {
		env["CLAUDE_MODE"] = claude.Mode
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
func BuildVolumes(cfg *config.Config, paths *config.Paths, env map[string]string, personalityDir string) ([]runtime.Volume, error) {
	var vols []runtime.Volume

	workspace := config.ExpandPath(cfg.Workspace)
	mountPath := workspace
	if cfg.WorktreePath != "" {
		mountPath = cfg.WorktreePath
	}
	vols = append(vols, runtime.Volume{
		HostPath:      mountPath,
		ContainerPath: "/workspace",
	})
	env["CLAUDE_WORKSPACE"] = "/workspace"

	// Mount the rendered container config YAML. The container reads this
	// instead of relying on 30+ individual environment variables.
	configPath := filepath.Join(paths.RenderedDir, "config.yaml")
	vols = append(vols, runtime.Volume{
		HostPath:      configPath,
		ContainerPath: "/etc/klaus/config.yaml",
		ReadOnly:      true,
	})
	env["KLAUS_CONFIG_FILE"] = "/etc/klaus/config.yaml"

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

	gitconfigVol, err := buildGitConfigVolume(cfg, paths, env)
	if err != nil {
		return nil, err
	}
	if gitconfigVol != nil {
		vols = append(vols, *gitconfigVol)
	}

	secretVols, err := resolveSecretFiles(cfg, paths)
	if err != nil {
		return nil, err
	}
	vols = append(vols, secretVols...)

	// Mount host sources config so klausctl inside the container can resolve
	// --source references (e.g. "klausctl push --source spiffy").
	if _, err := os.Stat(paths.SourcesFile); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("checking sources file: %w", err)
		}
	} else {
		vols = append(vols, runtime.Volume{
			HostPath:      paths.SourcesFile,
			ContainerPath: "/etc/klaus/sources.yaml",
			ReadOnly:      true,
		})
		// Set the container-internal path, overriding any host value from envForward.
		env["KLAUSCTL_SOURCES_FILE"] = "/etc/klaus/sources.yaml"
	}

	return vols, nil
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

func setGitEnvVars(env map[string]string, git *config.GitConfig) {
	if git.AuthorName != "" {
		env["GIT_AUTHOR_NAME"] = git.AuthorName
		env["GIT_COMMITTER_NAME"] = git.AuthorName
	}
	if git.AuthorEmail != "" {
		env["GIT_AUTHOR_EMAIL"] = git.AuthorEmail
		env["GIT_COMMITTER_EMAIL"] = git.AuthorEmail
	}
}

// BuildGitConfig generates a container-local gitconfig file content for
// credential helper and/or URL rewriting. Returns empty string if no
// gitconfig is needed.
func BuildGitConfig(git *config.GitConfig) string {
	if !git.HTTPSInsteadOfSSH && git.CredentialHelper == "" {
		return ""
	}

	var b strings.Builder

	if git.CredentialHelper == "gh" {
		b.WriteString("[credential \"https://github.com\"]\n")
		b.WriteString("\thelper = !/usr/bin/gh auth git-credential\n")
		b.WriteString("[credential \"https://gist.github.com\"]\n")
		b.WriteString("\thelper = !/usr/bin/gh auth git-credential\n")
	}

	if git.HTTPSInsteadOfSSH {
		b.WriteString("[url \"https://github.com/\"]\n")
		b.WriteString("\tinsteadOf = git@github.com:\n")
		b.WriteString("\tinsteadOf = ssh://git@github.com/\n")
	}

	return b.String()
}

func setEnvIfNotEmpty(env map[string]string, key, value string) {
	if value != "" {
		env[key] = value
	}
}

// buildGitConfigVolume writes a container-local gitconfig and returns a
// volume mount for it when git.credentialHelper or git.httpsInsteadOfSsh
// is configured. The GIT_CONFIG_GLOBAL env var is set to point at the
// mounted file so the workspace's .git/config is not modified.
func buildGitConfigVolume(cfg *config.Config, paths *config.Paths, env map[string]string) (*runtime.Volume, error) {
	content := BuildGitConfig(&cfg.Git)
	if content == "" {
		return nil, nil
	}

	if err := config.EnsureDir(paths.RenderedDir); err != nil {
		return nil, fmt.Errorf("creating rendered directory for gitconfig: %w", err)
	}

	hostPath := filepath.Join(paths.RenderedDir, "gitconfig")
	if err := os.WriteFile(hostPath, []byte(content), 0o600); err != nil {
		return nil, fmt.Errorf("writing gitconfig: %w", err)
	}

	env["GIT_CONFIG_GLOBAL"] = "/etc/klaus/gitconfig"
	return &runtime.Volume{
		HostPath:      hostPath,
		ContainerPath: "/etc/klaus/gitconfig",
		ReadOnly:      true,
	}, nil
}

// ResolveSecretRefs resolves all secret-related references in the config:
// McpServerRefs are merged into McpServers with optional Bearer tokens.
// This must be called before rendering so that the mcp-config.json is complete.
func ResolveSecretRefs(cfg *config.Config, paths *config.Paths) error {
	if len(cfg.McpServerRefs) == 0 {
		return nil
	}

	mcpStore, err := mcpserverstore.Load(paths.McpServersFile)
	if err != nil {
		return fmt.Errorf("loading managed MCP servers: %w", err)
	}

	var secretStore *secret.Store
	tokenStore := oauth.NewTokenStore(paths.TokensDir)
	for _, ref := range cfg.McpServerRefs {
		def, err := mcpStore.Get(ref)
		if err != nil {
			return fmt.Errorf("resolving mcpServerRef %q: %w", ref, err)
		}

		entry := map[string]any{
			"url":  def.URL,
			"type": "http",
		}

		if def.Secret != "" {
			if secretStore == nil {
				secretStore, err = secret.Load(paths.SecretsFile)
				if err != nil {
					return fmt.Errorf("loading secrets for MCP server refs: %w", err)
				}
			}
			token, err := secretStore.Get(def.Secret)
			if err != nil {
				return fmt.Errorf("resolving secret %q for MCP server %q: %w", def.Secret, ref, err)
			}
			entry["headers"] = map[string]string{
				"Authorization": "Bearer " + token,
			}
		} else if st := tokenStore.GetValidToken(def.URL); st != nil {
			entry["headers"] = map[string]string{
				"Authorization": "Bearer " + st.Token.AccessToken,
			}
		}

		if cfg.McpServers == nil {
			cfg.McpServers = make(map[string]any)
		}
		cfg.McpServers[ref] = entry
	}

	return nil
}

// resolveSecretFiles writes secret values to rendered/secrets/ and returns
// the volume mounts for them.
func resolveSecretFiles(cfg *config.Config, paths *config.Paths) ([]runtime.Volume, error) {
	if len(cfg.SecretFiles) == 0 {
		return nil, nil
	}

	store, err := secret.Load(paths.SecretsFile)
	if err != nil {
		return nil, fmt.Errorf("loading secrets for secret files: %w", err)
	}

	secretsDir := filepath.Join(paths.RenderedDir, "secrets")
	if err := config.EnsureDir(secretsDir); err != nil {
		return nil, fmt.Errorf("creating secrets directory: %w", err)
	}

	var vols []runtime.Volume
	for containerPath, secretName := range cfg.SecretFiles {
		if err := secret.ValidateName(secretName); err != nil {
			return nil, fmt.Errorf("secretFiles[%s]: %w", containerPath, err)
		}

		val, err := store.Get(secretName)
		if err != nil {
			return nil, fmt.Errorf("resolving secretFiles[%s]: %w", containerPath, err)
		}

		hostPath := filepath.Join(secretsDir, secretName)
		if err := os.WriteFile(hostPath, []byte(val), 0o600); err != nil {
			return nil, fmt.Errorf("writing secret file for %q: %w", secretName, err)
		}

		vols = append(vols, runtime.Volume{
			HostPath:      hostPath,
			ContainerPath: containerPath,
			ReadOnly:      true,
		})
	}

	return vols, nil
}
