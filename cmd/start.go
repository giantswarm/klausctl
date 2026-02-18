package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/oci"
	"github.com/giantswarm/klausctl/pkg/renderer"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

var startWorkspace string

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start a local klaus instance",
	Long: `Start a local klaus container with the configured settings.

This command:
  1. Loads configuration from ~/.config/klausctl/config.yaml
  2. Pulls OCI plugins (if configured)
  3. Renders configuration files (skills, settings, MCP config)
  4. Starts a container with the correct env vars, mounts, and ports`,
	RunE: runStart,
}

func init() {
	startCmd.Flags().StringVar(&startWorkspace, "workspace", "", "workspace directory to mount (overrides config file)")
	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, _ []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}

	// Override workspace from flag if provided.
	if startWorkspace != "" {
		cfg.Workspace = startWorkspace
	}

	// Validate that the workspace directory exists.
	workspace := config.ExpandPath(cfg.Workspace)
	if _, err := os.Stat(workspace); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("workspace directory does not exist: %s", workspace)
		}
		return fmt.Errorf("checking workspace directory: %w", err)
	}

	// Detect or validate container runtime.
	rt, err := runtime.New(cfg.Runtime)
	if err != nil {
		return err
	}
	if cfg.Runtime == "" {
		fmt.Fprintf(out, "Auto-detected %s runtime (set 'runtime' in config to override).\n", bold(rt.Name()))
	} else {
		fmt.Fprintf(out, "Using %s runtime.\n", rt.Name())
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	// Derive the instance name and container name consistently.
	const instanceName = "default"
	containerName := instance.ContainerName(instanceName)

	// Check if already running.
	inst, err := instance.Load(paths)
	if err == nil && inst.Name != "" {
		status, sErr := rt.Status(ctx, inst.ContainerName())
		if sErr == nil && status == "running" {
			return fmt.Errorf(
				"instance %q is already running (container: %s, MCP: http://localhost:%d)\nUse 'klausctl stop' to stop it first",
				inst.Name, inst.ContainerName(), inst.Port,
			)
		}
		// Clean up stale container.
		_ = rt.Remove(ctx, inst.ContainerName())
		_ = instance.Clear(paths)
	}

	// The image to use is cfg.Image (which defaults to the standard Klaus
	// image if not overridden by the user, e.g. with a toolchain image).
	image := cfg.Image

	// Render configuration files.
	r := renderer.New(paths)
	if err := r.Render(cfg); err != nil {
		return fmt.Errorf("rendering config: %w", err)
	}

	// Pull OCI plugins.
	if len(cfg.Plugins) > 0 {
		fmt.Fprintln(out, "Pulling plugins...")
		if err := oci.PullPlugins(ctx, cfg.Plugins, paths.PluginsDir, out); err != nil {
			return fmt.Errorf("pulling plugins: %w", err)
		}
	}

	// Build container run options.
	runOpts, err := buildRunOptions(cfg, paths, containerName, image)
	if err != nil {
		return fmt.Errorf("building run options: %w", err)
	}

	// Pull the image with streamed progress.
	fmt.Fprintf(out, "Pulling %s...\n", image)
	if err := rt.Pull(ctx, image, out); err != nil {
		return fmt.Errorf("pulling image: %w", err)
	}

	// Start container.
	fmt.Fprintln(out, "Starting klaus container...")
	containerID, err := rt.Run(ctx, runOpts)
	if err != nil {
		return fmt.Errorf("starting container: %w", err)
	}

	// Save instance state.
	inst = &instance.Instance{
		Name:        instanceName,
		ContainerID: containerID,
		Runtime:     rt.Name(),
		Image:       image,
		Port:        cfg.Port,
		Workspace:   workspace,
		StartedAt:   time.Now(),
	}
	if err := inst.Save(paths); err != nil {
		return fmt.Errorf("saving instance state: %w", err)
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, green("Klaus instance started."))
	fmt.Fprintf(out, "  Container:  %s\n", containerName)
	fmt.Fprintf(out, "  Image:      %s\n", image)
	fmt.Fprintf(out, "  Workspace:  %s\n", inst.Workspace)
	fmt.Fprintf(out, "  MCP:        http://localhost:%d\n", cfg.Port)

	// Warn about missing API key after the success context so it doesn't
	// appear before the user knows what's happening.
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		fmt.Fprintf(errOut, "\n%s ANTHROPIC_API_KEY is not set; the claude agent may fail to authenticate.\n", yellow("Warning:"))
	}

	fmt.Fprintf(out, "\nUse 'klausctl logs' to view output, 'klausctl stop' to stop.\n")
	return nil
}

// buildRunOptions constructs the container runtime options from config.
// This mirrors the Helm deployment.yaml template, producing the same
// env vars and volume mounts.
func buildRunOptions(cfg *config.Config, paths *config.Paths, containerName, image string) (runtime.RunOptions, error) {
	env, err := buildEnvVars(cfg, paths)
	if err != nil {
		return runtime.RunOptions{}, err
	}

	volumes := buildVolumes(cfg, paths, env)

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

// buildEnvVars constructs all container environment variables from config.
// These mirror the Helm deployment.yaml env section.
func buildEnvVars(cfg *config.Config, paths *config.Paths) (map[string]string, error) {
	env := make(map[string]string)

	// Internal port (always 8080 inside the container).
	env["PORT"] = "8080"

	// Forward ANTHROPIC_API_KEY from host (always).
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		env["ANTHROPIC_API_KEY"] = key
	}

	// Forward user-configured env vars from host.
	for _, name := range cfg.EnvForward {
		if val := os.Getenv(name); val != "" {
			env[name] = val
		}
	}

	// Set explicit env var overrides.
	for k, v := range cfg.EnvVars {
		env[k] = v
	}

	// Claude configuration.
	setClaudeEnvVars(env, &cfg.Claude)

	// Agents (JSON format via CLAUDE_AGENTS env var).
	if len(cfg.Agents) > 0 {
		agentsJSON, err := json.Marshal(cfg.Agents)
		if err != nil {
			return nil, fmt.Errorf("marshaling agents: %w", err)
		}
		env["CLAUDE_AGENTS"] = string(agentsJSON)
	}

	return env, nil
}

// setClaudeEnvVars populates Claude Code agent env vars from the config.
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

// buildVolumes constructs the container volume mounts and sets related env vars.
// The env map is mutated to add mount-dependent env vars (CLAUDE_WORKSPACE, etc.).
func buildVolumes(cfg *config.Config, paths *config.Paths, env map[string]string) []runtime.Volume {
	var vols []runtime.Volume

	// Workspace mount.
	workspace := config.ExpandPath(cfg.Workspace)
	vols = append(vols, runtime.Volume{
		HostPath:      workspace,
		ContainerPath: "/workspace",
	})
	env["CLAUDE_WORKSPACE"] = "/workspace"

	// MCP config mount.
	if len(cfg.McpServers) > 0 {
		mcpConfigPath := filepath.Join(paths.RenderedDir, "mcp-config.json")
		vols = append(vols, runtime.Volume{
			HostPath:      mcpConfigPath,
			ContainerPath: "/etc/klaus/mcp-config.json",
			ReadOnly:      true,
		})
		env["CLAUDE_MCP_CONFIG"] = "/etc/klaus/mcp-config.json"
	}

	// Settings mount (hooks or settingsFile -- mutually exclusive, validated in config).
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

	// Hook scripts mount.
	for name := range cfg.HookScripts {
		hostPath := filepath.Join(paths.RenderedDir, "hooks", name)
		vols = append(vols, runtime.Volume{
			HostPath:      hostPath,
			ContainerPath: "/etc/klaus/hooks/" + name,
			ReadOnly:      true,
		})
	}

	// Extensions mount (skills and agent files).
	if renderer.HasExtensions(cfg) {
		vols = append(vols, runtime.Volume{
			HostPath:      paths.ExtensionsDir,
			ContainerPath: "/etc/klaus/extensions",
			ReadOnly:      true,
		})
	}

	// CLAUDE_ADD_DIRS: aggregate extensions dir + user addDirs.
	addDirs := buildAddDirs(cfg)
	if len(addDirs) > 0 {
		env["CLAUDE_ADD_DIRS"] = strings.Join(addDirs, ",")
		if cfg.Claude.LoadAdditionalDirsMemory == nil || *cfg.Claude.LoadAdditionalDirsMemory {
			env["CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD"] = "true"
		}
	}

	// Plugin mounts and CLAUDE_PLUGIN_DIRS.
	pluginDirs := buildPluginDirs(cfg)
	if len(pluginDirs) > 0 {
		env["CLAUDE_PLUGIN_DIRS"] = strings.Join(pluginDirs, ",")
	}

	// Mount each plugin directory.
	for _, p := range cfg.Plugins {
		shortName := oci.ShortPluginName(p.Repository)
		hostPath := filepath.Join(paths.PluginsDir, shortName)
		vols = append(vols, runtime.Volume{
			HostPath:      hostPath,
			ContainerPath: "/var/lib/klaus/plugins/" + shortName,
			ReadOnly:      true,
		})
	}

	return vols
}

// buildAddDirs aggregates CLAUDE_ADD_DIRS from extensions and user config.
func buildAddDirs(cfg *config.Config) []string {
	var dirs []string
	if renderer.HasExtensions(cfg) {
		dirs = append(dirs, "/etc/klaus/extensions")
	}
	dirs = append(dirs, cfg.Claude.AddDirs...)
	return dirs
}

// buildPluginDirs aggregates CLAUDE_PLUGIN_DIRS from plugins and user config.
func buildPluginDirs(cfg *config.Config) []string {
	var dirs []string
	dirs = append(dirs, cfg.Claude.PluginDirs...)
	dirs = append(dirs, oci.PluginDirs(cfg.Plugins)...)
	return dirs
}

func setEnvIfNotEmpty(env map[string]string, key, value string) {
	if value != "" {
		env[key] = value
	}
}
