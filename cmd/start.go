package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/oci"
	"github.com/giantswarm/klausctl/pkg/renderer"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

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
	rootCmd.AddCommand(startCmd)
}

func runStart(_ *cobra.Command, _ []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}

	// Detect or validate container runtime.
	rt, err := runtime.New(cfg.Runtime)
	if err != nil {
		return err
	}
	fmt.Printf("Using %s runtime.\n", rt.Name())

	paths := config.DefaultPaths()

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

	// Render configuration files.
	r := renderer.New(paths)
	if err := r.Render(cfg); err != nil {
		return fmt.Errorf("rendering config: %w", err)
	}

	// Pull OCI plugins.
	if len(cfg.Plugins) > 0 {
		fmt.Println("Pulling plugins...")
		if err := oci.PullPlugins(cfg.Plugins, paths.PluginsDir); err != nil {
			return fmt.Errorf("pulling plugins: %w", err)
		}
	}

	// Build container run options.
	runOpts, err := buildRunOptions(cfg, paths)
	if err != nil {
		return fmt.Errorf("building run options: %w", err)
	}

	// Start container.
	fmt.Printf("Starting klaus container from %s...\n", cfg.Image)
	containerID, err := rt.Run(ctx, runOpts)
	if err != nil {
		return fmt.Errorf("starting container: %w", err)
	}

	// Save instance state.
	inst = &instance.Instance{
		Name:        "default",
		ContainerID: containerID,
		Runtime:     rt.Name(),
		Image:       cfg.Image,
		Port:        cfg.Port,
		Workspace:   config.ExpandPath(cfg.Workspace),
	}
	if err := inst.Save(paths); err != nil {
		return fmt.Errorf("saving instance state: %w", err)
	}

	fmt.Println()
	fmt.Println("Klaus instance started.")
	fmt.Printf("  Container:  %s\n", inst.ContainerName())
	fmt.Printf("  Image:      %s\n", cfg.Image)
	fmt.Printf("  Workspace:  %s\n", inst.Workspace)
	fmt.Printf("  MCP:        http://localhost:%d\n", cfg.Port)
	fmt.Printf("\nUse 'klausctl logs' to view output, 'klausctl stop' to stop.\n")
	return nil
}

// buildRunOptions constructs the container runtime options from config.
// This mirrors the Helm deployment.yaml template, producing the same
// env vars and volume mounts.
func buildRunOptions(cfg *config.Config, paths *config.Paths) (runtime.RunOptions, error) {
	opts := runtime.RunOptions{
		Name:    "klausctl-default",
		Image:   cfg.Image,
		Detach:  true,
		EnvVars: make(map[string]string),
		Ports:   map[int]int{cfg.Port: 8080},
	}

	// --- Environment Variables ---
	// These mirror the Helm deployment.yaml env section.

	// Internal port (always 8080 inside the container).
	opts.EnvVars["PORT"] = "8080"

	// Forward ANTHROPIC_API_KEY from host (always).
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		opts.EnvVars["ANTHROPIC_API_KEY"] = key
	}

	// Forward user-configured env vars from host.
	for _, name := range cfg.EnvForward {
		if val := os.Getenv(name); val != "" {
			opts.EnvVars[name] = val
		}
	}

	// Set explicit env var overrides.
	for k, v := range cfg.EnvVars {
		opts.EnvVars[k] = v
	}

	// Claude configuration.
	setEnvIfNotEmpty(opts.EnvVars, "CLAUDE_MODEL", cfg.Claude.Model)
	setEnvIfNotEmpty(opts.EnvVars, "CLAUDE_SYSTEM_PROMPT", cfg.Claude.SystemPrompt)
	setEnvIfNotEmpty(opts.EnvVars, "CLAUDE_APPEND_SYSTEM_PROMPT", cfg.Claude.AppendSystemPrompt)
	setEnvIfNotEmpty(opts.EnvVars, "CLAUDE_PERMISSION_MODE", cfg.Claude.PermissionMode)
	setEnvIfNotEmpty(opts.EnvVars, "CLAUDE_EFFORT", cfg.Claude.Effort)
	setEnvIfNotEmpty(opts.EnvVars, "CLAUDE_FALLBACK_MODEL", cfg.Claude.FallbackModel)
	setEnvIfNotEmpty(opts.EnvVars, "CLAUDE_ACTIVE_AGENT", cfg.Claude.ActiveAgent)

	if cfg.Claude.MaxTurns > 0 {
		opts.EnvVars["CLAUDE_MAX_TURNS"] = fmt.Sprintf("%d", cfg.Claude.MaxTurns)
	}
	if cfg.Claude.MaxBudgetUSD > 0 {
		opts.EnvVars["CLAUDE_MAX_BUDGET_USD"] = fmt.Sprintf("%.2f", cfg.Claude.MaxBudgetUSD)
	}
	if cfg.Claude.StrictMcpConfig {
		opts.EnvVars["CLAUDE_STRICT_MCP_CONFIG"] = "true"
	}
	if cfg.Claude.PersistentMode {
		opts.EnvVars["CLAUDE_PERSISTENT_MODE"] = "true"
	}
	if cfg.Claude.NoSessionPersistence != nil && *cfg.Claude.NoSessionPersistence {
		opts.EnvVars["CLAUDE_NO_SESSION_PERSISTENCE"] = "true"
	}

	if len(cfg.Claude.Tools) > 0 {
		opts.EnvVars["CLAUDE_TOOLS"] = strings.Join(cfg.Claude.Tools, ",")
	}
	if len(cfg.Claude.AllowedTools) > 0 {
		opts.EnvVars["CLAUDE_ALLOWED_TOOLS"] = strings.Join(cfg.Claude.AllowedTools, ",")
	}
	if len(cfg.Claude.DisallowedTools) > 0 {
		opts.EnvVars["CLAUDE_DISALLOWED_TOOLS"] = strings.Join(cfg.Claude.DisallowedTools, ",")
	}

	// Agents (JSON format via CLAUDE_AGENTS env var).
	if len(cfg.Agents) > 0 {
		agentsJSON, err := json.Marshal(cfg.Agents)
		if err != nil {
			return opts, fmt.Errorf("marshaling agents: %w", err)
		}
		opts.EnvVars["CLAUDE_AGENTS"] = string(agentsJSON)
	}

	// --- Volume Mounts ---

	// Workspace mount.
	workspace := config.ExpandPath(cfg.Workspace)
	opts.Volumes = append(opts.Volumes, runtime.Volume{
		HostPath:      workspace,
		ContainerPath: "/workspace",
	})
	opts.EnvVars["CLAUDE_WORKSPACE"] = "/workspace"

	// MCP config mount.
	if len(cfg.McpServers) > 0 {
		mcpConfigPath := filepath.Join(paths.RenderedDir, "mcp-config.json")
		opts.Volumes = append(opts.Volumes, runtime.Volume{
			HostPath:      mcpConfigPath,
			ContainerPath: "/etc/klaus/mcp-config.json",
			ReadOnly:      true,
		})
		opts.EnvVars["CLAUDE_MCP_CONFIG"] = "/etc/klaus/mcp-config.json"
	}

	// Settings mount (hooks).
	if len(cfg.Hooks) > 0 {
		settingsPath := filepath.Join(paths.RenderedDir, "settings.json")
		opts.Volumes = append(opts.Volumes, runtime.Volume{
			HostPath:      settingsPath,
			ContainerPath: "/etc/klaus/settings.json",
			ReadOnly:      true,
		})
		opts.EnvVars["CLAUDE_SETTINGS_FILE"] = "/etc/klaus/settings.json"
	}

	// Hook scripts mount.
	for name := range cfg.HookScripts {
		hostPath := filepath.Join(paths.RenderedDir, "hooks", name)
		opts.Volumes = append(opts.Volumes, runtime.Volume{
			HostPath:      hostPath,
			ContainerPath: "/etc/klaus/hooks/" + name,
			ReadOnly:      true,
		})
	}

	// Extensions mount (skills and agent files).
	if renderer.HasExtensions(cfg) {
		opts.Volumes = append(opts.Volumes, runtime.Volume{
			HostPath:      paths.ExtensionsDir,
			ContainerPath: "/etc/klaus/extensions",
			ReadOnly:      true,
		})
	}

	// CLAUDE_ADD_DIRS: aggregate extensions dir + user addDirs.
	addDirs := buildAddDirs(cfg)
	if len(addDirs) > 0 {
		opts.EnvVars["CLAUDE_ADD_DIRS"] = strings.Join(addDirs, ",")
		opts.EnvVars["CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD"] = "true"
	}

	// Plugin mounts and CLAUDE_PLUGIN_DIRS.
	pluginDirs := buildPluginDirs(cfg)
	if len(pluginDirs) > 0 {
		opts.EnvVars["CLAUDE_PLUGIN_DIRS"] = strings.Join(pluginDirs, ",")
	}

	// Mount each plugin directory.
	for _, p := range cfg.Plugins {
		shortName := oci.ShortPluginName(p.Repository)
		hostPath := filepath.Join(paths.PluginsDir, shortName)
		opts.Volumes = append(opts.Volumes, runtime.Volume{
			HostPath:      hostPath,
			ContainerPath: "/mnt/plugins/" + shortName,
			ReadOnly:      true,
		})
	}

	return opts, nil
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
