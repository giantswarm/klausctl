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

	klausoci "github.com/giantswarm/klaus-oci"
	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/orchestrator"
)

var (
	pluginValidateOut string
	pluginPullOut     string
	pluginPullSource  string
	pluginPushOut     string
	pluginPushSource  string
	pluginPushDryRun  bool
	pluginListOut     string
	pluginListLocal   bool
	pluginListSource  string
	pluginListAll     bool
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

Accepts a short name, short name with tag, or full OCI reference:

  klausctl plugin pull gs-base              (resolves latest version)
  klausctl plugin pull gs-base:v0.0.7       (specific version)
  klausctl plugin pull gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base:v0.0.7`,
	Args: cobra.ExactArgs(1),
	RunE: runPluginPull,
}

var pluginPushCmd = &cobra.Command{
	Use:   "push <directory> <reference>",
	Short: "Push a plugin to the OCI registry",
	Long: `Push a local plugin directory as an OCI artifact to the registry.

The directory must contain valid plugin content (skills/, agents/, hooks/,
or .mcp.json) and a .claude-plugin/plugin.json manifest.

Accepts a full OCI reference with tag or a short name with tag:

  klausctl plugin push ./my-plugin gs-base:v1.0.0
  klausctl plugin push ./my-plugin gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base:v1.0.0`,
	Args: cobra.ExactArgs(2),
	RunE: runPluginPush,
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
	pluginPullCmd.Flags().StringVar(&pluginPullSource, "source", "", "resolve against a specific source")
	pluginPushCmd.Flags().StringVarP(&pluginPushOut, "output", "o", "text", "output format: text, json")
	pluginPushCmd.Flags().StringVar(&pluginPushSource, "source", "", "use a specific source registry for the push destination")
	pluginPushCmd.Flags().BoolVar(&pluginPushDryRun, "dry-run", false, "validate and resolve without pushing")
	pluginListCmd.Flags().StringVarP(&pluginListOut, "output", "o", "text", "output format: text, json")
	pluginListCmd.Flags().BoolVar(&pluginListLocal, "local", false, "list only locally cached plugins")
	pluginListCmd.Flags().StringVar(&pluginListSource, "source", "", "list plugins from a specific source only")
	pluginListCmd.Flags().BoolVar(&pluginListAll, "all", false, "list plugins from all configured sources")

	pluginCmd.AddCommand(pluginValidateCmd)
	pluginCmd.AddCommand(pluginPullCmd)
	pluginCmd.AddCommand(pluginPushCmd)
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

// pullPluginFn wraps the typed PullPlugin method for use with pullArtifact.
var pullPluginFn pullFn = func(ctx context.Context, client *klausoci.Client, ref, destDir string) (string, bool, error) {
	result, err := client.PullPlugin(ctx, ref, destDir)
	if err != nil {
		return "", false, err
	}
	return result.Digest, result.Cached, nil
}

// listPluginsFn wraps the typed ListPlugins method for use with listLatestRemoteArtifacts.
var listPluginsFn listFn = func(ctx context.Context, client *klausoci.Client, opts ...klausoci.ListOption) ([]klausoci.ListEntry, error) {
	return client.ListPlugins(ctx, opts...)
}

// pushPluginFn reads plugin metadata from sourceDir and pushes it as an OCI artifact.
var pushPluginFn pushFn = func(ctx context.Context, client *klausoci.Client, sourceDir, ref string) (string, error) {
	plugin, err := klausoci.ReadPluginFromDir(sourceDir)
	if err != nil {
		return "", err
	}
	result, err := client.PushPlugin(ctx, sourceDir, ref, *plugin)
	if err != nil {
		return "", err
	}
	return result.Digest, nil
}

func runPluginPush(cmd *cobra.Command, args []string) error {
	if err := validateOutputFormat(pluginPushOut); err != nil {
		return err
	}

	dir := args[0]
	if err := validatePluginDir(dir, io.Discard, "text"); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	resolver, err := buildSourceResolver(pluginPushSource)
	if err != nil {
		return err
	}

	ref := resolver.ResolvePluginRef(args[1])
	if err := validatePushRef(ref); err != nil {
		return err
	}

	return pushArtifact(ctx, dir, ref, pushPluginFn, cmd.OutOrStdout(), pluginPushOut, pushOpts{dryRun: pluginPushDryRun})
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

	resolver, err := buildSourceResolver(pluginPullSource)
	if err != nil {
		return err
	}

	resolved := resolver.ResolvePluginRef(args[0])
	client := orchestrator.NewDefaultClient()
	ref, err := client.ResolvePluginRef(ctx, resolved)
	if err != nil {
		return err
	}

	return pullArtifact(ctx, ref, paths.PluginsDir, pullPluginFn, cmd.OutOrStdout(), pluginPullOut)
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

	resolver, err := buildListSourceResolver(pluginListSource, pluginListAll)
	if err != nil {
		return err
	}

	return listOCIArtifacts(ctx, cmd.OutOrStdout(), paths.PluginsDir, pluginListOut, "plugin", "plugins", resolver.PluginRegistries(), pluginListLocal, listPluginsFn)
}
