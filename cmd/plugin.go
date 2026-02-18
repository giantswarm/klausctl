package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/oci"
)

var (
	pluginValidateOut string
	pluginPullOut     string
	pluginListOut     string
	pluginListLocal   bool
)

var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Manage OCI plugins",
	Long: `Commands for working with klaus OCI plugins.

Plugins are OCI artifacts containing skills, hooks, agents, and MCP server
configurations. They are published to the registry by CI and can be pulled
locally for use with klausctl.`,
}

var pluginValidateCmd = &cobra.Command{
	Use:   "validate <directory>",
	Short: "Validate a local plugin directory",
	Long: `Validate a local plugin directory against the expected structure.

A valid plugin directory must contain at least one of:
  - skills/     (with SKILL.md files)
  - agents/     (agent definition files)
  - hooks/      (hook configuration)
  - .mcp.json   (MCP server configuration)`,
	Args: cobra.ExactArgs(1),
	RunE: runPluginValidate,
}

var pluginPullCmd = &cobra.Command{
	Use:   "pull <reference>",
	Short: "Pull a plugin from the OCI registry",
	Long: `Pull a plugin OCI artifact from the registry to the local cache.

The reference must include a tag or digest:

  klausctl plugin pull gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base:v0.6.0`,
	Args: cobra.ExactArgs(1),
	RunE: runPluginPull,
}

var pluginListCmd = &cobra.Command{
	Use:   "list",
	Short: "List plugins",
	Long: `List available plugins from the remote OCI registry.

By default, discovers plugins from the registry, shows the latest version
of each, and indicates whether it is cached locally.

With --local, shows only locally cached plugins with full detail.`,
	RunE: runPluginList,
}

// pluginValidation is the JSON representation of a successful plugin validation.
type pluginValidation struct {
	Valid     bool     `json:"valid"`
	Directory string   `json:"directory"`
	Found     []string `json:"found"`
}

func init() {
	pluginValidateCmd.Flags().StringVarP(&pluginValidateOut, "output", "o", "text", "output format: text, json")
	pluginPullCmd.Flags().StringVarP(&pluginPullOut, "output", "o", "text", "output format: text, json")
	pluginListCmd.Flags().StringVarP(&pluginListOut, "output", "o", "text", "output format: text, json")
	pluginListCmd.Flags().BoolVar(&pluginListLocal, "local", false, "list only locally cached plugins")

	pluginCmd.AddCommand(pluginValidateCmd)
	pluginCmd.AddCommand(pluginPullCmd)
	pluginCmd.AddCommand(pluginListCmd)
	rootCmd.AddCommand(pluginCmd)
}

func runPluginValidate(cmd *cobra.Command, args []string) error {
	if err := validateOutputFormat(pluginValidateOut); err != nil {
		return err
	}
	return validatePluginDir(args[0], cmd.OutOrStdout(), pluginValidateOut)
}

// validatePluginDir checks that a directory has a valid plugin structure.
func validatePluginDir(dir string, out io.Writer, outputFmt string) error {
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("directory does not exist: %s", dir)
		}
		return fmt.Errorf("checking directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", dir)
	}

	recognized := []string{"skills", "agents", "hooks", ".mcp.json"}
	var found []string
	for _, name := range recognized {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			found = append(found, name)
		}
	}

	if len(found) == 0 {
		return fmt.Errorf("no recognized plugin content found in %s\nExpected at least one of: skills/, agents/, hooks/, .mcp.json", dir)
	}

	if outputFmt == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(pluginValidation{
			Valid:     true,
			Directory: dir,
			Found:     found,
		})
	}

	fmt.Fprintf(out, "Valid plugin directory: %s\n", dir)
	fmt.Fprintf(out, "  Found: %v\n", found)
	return nil
}

func runPluginPull(cmd *cobra.Command, args []string) error {
	if err := validateOutputFormat(pluginPullOut); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	if err := config.EnsureDir(paths.PluginsDir); err != nil {
		return fmt.Errorf("creating plugins directory: %w", err)
	}

	return pullArtifact(ctx, args[0], paths.PluginsDir, oci.PluginArtifact, cmd.OutOrStdout(), pluginPullOut)
}

func runPluginList(cmd *cobra.Command, _ []string) error {
	if err := validateOutputFormat(pluginListOut); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	return listOCIArtifacts(ctx, cmd.OutOrStdout(), paths.PluginsDir, pluginListOut, "plugin", "plugins", oci.DefaultPluginRegistry, pluginListLocal)
}
