package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/oci"
)

var (
	pluginListOut    string
	pluginListRemote bool
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
	Long: `List locally cached plugins, or query the remote registry with --remote.

Without --remote, shows plugins downloaded to the local cache.
With --remote, shows available tags for locally cached plugin repositories.`,
	RunE: runPluginList,
}

func init() {
	pluginListCmd.Flags().StringVarP(&pluginListOut, "output", "o", "text", "output format: text, json")
	pluginListCmd.Flags().BoolVar(&pluginListRemote, "remote", false, "list remote registry tags instead of local cache")

	pluginCmd.AddCommand(pluginValidateCmd)
	pluginCmd.AddCommand(pluginPullCmd)
	pluginCmd.AddCommand(pluginListCmd)
	rootCmd.AddCommand(pluginCmd)
}

func runPluginValidate(_ *cobra.Command, args []string) error {
	dir := args[0]
	return validatePluginDir(dir)
}

// validatePluginDir checks that a directory has a valid plugin structure.
func validatePluginDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
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

	fmt.Printf("Valid plugin directory: %s\n", dir)
	fmt.Printf("  Found: %v\n", found)
	return nil
}

func runPluginPull(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	if err := config.EnsureDir(paths.PluginsDir); err != nil {
		return fmt.Errorf("creating plugins directory: %w", err)
	}

	return pullArtifact(ctx, args[0], paths.PluginsDir, oci.PluginArtifact, cmd.OutOrStdout())
}

func runPluginList(cmd *cobra.Command, _ []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	out := cmd.OutOrStdout()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	if pluginListRemote {
		tags, err := listRemoteTags(ctx, paths.PluginsDir)
		if err != nil {
			return err
		}
		if len(tags) == 0 {
			if pluginListOut != "json" {
				fmt.Fprintln(out, "No locally cached plugins to query remote tags for.")
				fmt.Fprintln(out, "Use 'klausctl plugin pull <ref>' to pull a plugin first.")
			} else {
				fmt.Fprintln(out, "[]")
			}
			return nil
		}
		return printRemoteTags(out, tags, pluginListOut)
	}

	artifacts, err := listLocalArtifacts(paths.PluginsDir)
	if err != nil {
		return err
	}

	if len(artifacts) == 0 && pluginListOut != "json" {
		fmt.Fprintln(out, "No plugins cached locally.")
		fmt.Fprintln(out, "Use 'klausctl plugin pull <ref>' to pull a plugin.")
		return nil
	}

	return printLocalArtifacts(out, artifacts, pluginListOut)
}
