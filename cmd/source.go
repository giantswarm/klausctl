package cmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
)

var (
	sourceAddRegistry      string
	sourceAddToolchains    string
	sourceAddPersonalities string
	sourceAddPlugins       string
	sourceAddDefault       bool
)

var sourceCmd = &cobra.Command{
	Use:   "source",
	Short: "Manage artifact sources",
	Long: `Manage named OCI registry sources for toolchains, personalities, and plugins.

Sources define where klausctl looks for artifacts when resolving short names.
The built-in "giantswarm" source is always present and cannot be removed.`,
}

var sourceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured sources",
	RunE:  runSourceList,
}

var sourceAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a new artifact source",
	Long: `Add a new named OCI registry source.

Artifact type paths are derived by convention from the registry base:
  - Toolchains:    <registry>/klaus-toolchains/<name>
  - Personalities: <registry>/klaus-personalities/<name>
  - Plugins:       <registry>/klaus-plugins/<name>

Use --toolchains, --personalities, or --plugins to override individual paths.`,
	Args: cobra.ExactArgs(1),
	RunE: runSourceAdd,
}

var sourceRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a source",
	Long:  `Remove a named source. The built-in "giantswarm" source cannot be removed.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runSourceRemove,
}

var sourceSetDefaultCmd = &cobra.Command{
	Use:   "set-default <name>",
	Short: "Set the default source",
	Long:  `Set the named source as the default for short-name resolution.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runSourceSetDefault,
}

func init() {
	sourceAddCmd.Flags().StringVar(&sourceAddRegistry, "registry", "", "registry base URL (required)")
	sourceAddCmd.Flags().StringVar(&sourceAddToolchains, "toolchains", "", "override toolchain registry path")
	sourceAddCmd.Flags().StringVar(&sourceAddPersonalities, "personalities", "", "override personality registry path")
	sourceAddCmd.Flags().StringVar(&sourceAddPlugins, "plugins", "", "override plugin registry path")
	sourceAddCmd.Flags().BoolVar(&sourceAddDefault, "default", false, "set as the default source")
	_ = sourceAddCmd.MarkFlagRequired("registry")

	sourceCmd.AddCommand(sourceListCmd)
	sourceCmd.AddCommand(sourceAddCmd)
	sourceCmd.AddCommand(sourceRemoveCmd)
	sourceCmd.AddCommand(sourceSetDefaultCmd)
	rootCmd.AddCommand(sourceCmd)
}

func loadSourceConfig() (*config.SourceConfig, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil, err
	}
	return config.LoadSourceConfig(paths.SourcesFile)
}

// buildSourceResolver creates a SourceResolver from the current sources config.
// If sourceFilter is non-empty, the resolver is restricted to that single source.
func buildSourceResolver(sourceFilter string) (*config.SourceResolver, error) {
	sc, err := loadSourceConfig()
	if err != nil {
		return nil, err
	}
	resolver := config.NewSourceResolver(sc.Sources)
	if sourceFilter != "" {
		return resolver.ForSource(sourceFilter)
	}
	return resolver, nil
}

func runSourceList(cmd *cobra.Command, _ []string) error {
	sc, err := loadSourceConfig()
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	w := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tREGISTRY\tDEFAULT")
	for _, s := range sc.Sources {
		def := ""
		if s.Default {
			def = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", s.Name, s.Registry, def)
	}
	return w.Flush()
}

func runSourceAdd(cmd *cobra.Command, args []string) error {
	sc, err := loadSourceConfig()
	if err != nil {
		return err
	}

	s := config.Source{
		Name:          args[0],
		Registry:      sourceAddRegistry,
		Toolchains:    sourceAddToolchains,
		Personalities: sourceAddPersonalities,
		Plugins:       sourceAddPlugins,
	}

	if err := sc.Add(s); err != nil {
		return err
	}

	if sourceAddDefault {
		if err := sc.SetDefault(s.Name); err != nil {
			return err
		}
	}

	if err := sc.Save(); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Added source %q (%s)\n", s.Name, s.Registry)
	return nil
}

func runSourceRemove(cmd *cobra.Command, args []string) error {
	sc, err := loadSourceConfig()
	if err != nil {
		return err
	}

	if err := sc.Remove(args[0]); err != nil {
		return err
	}

	if err := sc.Save(); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Removed source %q\n", args[0])
	return nil
}

func runSourceSetDefault(cmd *cobra.Command, args []string) error {
	sc, err := loadSourceConfig()
	if err != nil {
		return err
	}

	if err := sc.SetDefault(args[0]); err != nil {
		return err
	}

	if err := sc.Save(); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Default source set to %q\n", args[0])
	return nil
}
