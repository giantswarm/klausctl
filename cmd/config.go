package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
	Long:  `Manage the klausctl configuration file.`,
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a default configuration file",
	Long:  `Create a default configuration file at ~/.config/klausctl/instances/default/config.yaml.`,
	RunE:  runConfigInit,
}

var configShowEffective bool

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current configuration",
	Long: `Display the current configuration file contents.

Use --effective to show the resolved configuration with all defaults applied.`,
	RunE: runConfigShow,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Show configuration file path",
	Long:  `Print the path to the configuration file.`,
	RunE:  runConfigPath,
}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration file syntax",
	Long:  `Parse and validate the configuration file, reporting any errors.`,
	RunE:  runConfigValidate,
}

func init() {
	configShowCmd.Flags().BoolVar(&configShowEffective, "effective", false, "show resolved config with defaults applied")

	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configPathCmd)
	configCmd.AddCommand(configValidateCmd)
	rootCmd.AddCommand(configCmd)
}

// resolvedConfigFile returns the config file path, respecting the --config flag.
func resolvedConfigFile() (string, error) {
	if cfgFile != "" {
		return config.ExpandPath(cfgFile), nil
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return "", err
	}
	return paths.ConfigFile, nil
}

func runConfigInit(cmd *cobra.Command, _ []string) error {
	path, err := resolvedConfigFile()
	if err != nil {
		return err
	}

	// Check if config already exists.
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("config file already exists: %s\nUse 'klausctl config show' to view it", path)
	}

	if err := config.EnsureDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	defaultCfg := defaultConfigTemplate()
	if err := os.WriteFile(path, []byte(defaultCfg), 0o644); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Config file created: %s\n", path)
	fmt.Fprintln(out, "Edit the file to configure your workspace and preferences.")
	return nil
}

func runConfigShow(cmd *cobra.Command, _ []string) error {
	path, err := resolvedConfigFile()
	if err != nil {
		return err
	}

	if configShowEffective {
		cfg, err := config.Load(path)
		if err != nil {
			return err
		}
		data, err := cfg.Marshal()
		if err != nil {
			return fmt.Errorf("marshaling config: %w", err)
		}
		fmt.Fprint(cmd.OutOrStdout(), string(data))
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("config file not found: %s\nRun 'klausctl config init' to create one", path)
		}
		return fmt.Errorf("reading config: %w", err)
	}

	fmt.Fprint(cmd.OutOrStdout(), string(data))
	return nil
}

func runConfigPath(cmd *cobra.Command, _ []string) error {
	path, err := resolvedConfigFile()
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), path)
	return nil
}

func runConfigValidate(cmd *cobra.Command, _ []string) error {
	path, err := resolvedConfigFile()
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()

	_, err = config.Load(path)
	if err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	fmt.Fprintf(out, "Config file is valid: %s\n", path)
	return nil
}

func defaultConfigTemplate() string {
	return `# klausctl configuration
# See: https://github.com/giantswarm/klausctl

# Container runtime (auto-detected if not set)
# runtime: docker  # or: podman

# Personality: an OCI artifact defining the AI's identity (SOUL.md) and
# a curated set of plugins. The personality may also specify a default image.
# Instance-level image and plugins compose with and can override personality values.
# personality: gsoci.azurecr.io/giantswarm/klaus-personalities/sre:v1.0.0

# Klaus container image (overrides personality image if set)
image: gsoci.azurecr.io/giantswarm/klaus:latest

# Workspace directory to mount into the container
workspace: ~/projects

# Host port for the MCP endpoint
port: 8080

# Claude Code agent configuration
claude:
  # model: sonnet
  # systemPrompt: "You are a helpful coding assistant."
  # maxBudgetUsd: 5.0
  permissionMode: bypassPermissions

# Forward host environment variables to the container
# (ANTHROPIC_API_KEY is always forwarded if set)
# envForward:
#   - GITHUB_TOKEN

# Inline skills
# skills:
#   api-conventions:
#     description: "API design patterns"
#     content: |
#       When writing API endpoints...

# Subagents (JSON format, highest priority)
# agents:
#   reviewer:
#     description: "Reviews code changes"
#     prompt: "You are a senior code reviewer..."

# Lifecycle hooks (rendered to settings.json)
# hooks:
#   PreToolUse:
#     - matcher: "Bash"
#       hooks:
#         - type: command
#           command: /etc/klaus/hooks/block-dangerous.sh

# MCP servers
# mcpServers:
#   github:
#     type: http
#     url: https://api.githubcopilot.com/mcp/
#     headers:
#       Authorization: "Bearer ${GITHUB_TOKEN}"

# OCI plugins (pulled before container start)
# plugins:
#   - repository: gsoci.azurecr.io/giantswarm/klaus-plugins/gs-platform
#     tag: v1.2.0
`
}
