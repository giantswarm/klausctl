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

	sourceUpdateRegistry      string
	sourceUpdateToolchains    string
	sourceUpdatePersonalities string
	sourceUpdatePlugins       string
)

var sourceCmd = &cobra.Command{
	Use:   "source",
	Short: "Manage artifact sources",
	Long: `Manage named OCI registry sources for toolchains, personalities, and plugins.

Sources define where klausctl looks for artifacts when resolving short names.
The built-in "giantswarm" source is always present and cannot be removed.

Configuration is stored in: ~/.config/klausctl/sources.yaml`,
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
	Example: `  klausctl source add my-team --registry my-registry.io/my-team
  klausctl source add my-team --registry my-registry.io/my-team --default
  klausctl source add custom --registry custom.io/org --toolchains custom.io/org/tools`,
	Args: cobra.ExactArgs(1),
	RunE: runSourceAdd,
}

var sourceUpdateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Update an existing source",
	Long: `Update the registry URL or artifact path overrides for an existing source.

Only the flags you provide are changed; other fields are preserved.`,
	Example: `  klausctl source update my-team --registry new-registry.io/my-team
  klausctl source update my-team --toolchains new-registry.io/my-team/custom-tools`,
	Args: cobra.ExactArgs(1),
	RunE: runSourceUpdate,
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

var sourceShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show details of a source including derived registry paths",
	Long: `Show the full configuration of a named source, including the derived
registry paths for toolchains, personalities, and plugins.`,
	Args: cobra.ExactArgs(1),
	RunE: runSourceShow,
}

func init() {
	sourceAddCmd.Flags().StringVar(&sourceAddRegistry, "registry", "", "registry base URL (required)")
	sourceAddCmd.Flags().StringVar(&sourceAddToolchains, "toolchains", "", "override toolchain registry path")
	sourceAddCmd.Flags().StringVar(&sourceAddPersonalities, "personalities", "", "override personality registry path")
	sourceAddCmd.Flags().StringVar(&sourceAddPlugins, "plugins", "", "override plugin registry path")
	sourceAddCmd.Flags().BoolVar(&sourceAddDefault, "default", false, "set as the default source")
	_ = sourceAddCmd.MarkFlagRequired("registry")

	sourceUpdateCmd.Flags().StringVar(&sourceUpdateRegistry, "registry", "", "update registry base URL")
	sourceUpdateCmd.Flags().StringVar(&sourceUpdateToolchains, "toolchains", "", "update toolchain registry path override")
	sourceUpdateCmd.Flags().StringVar(&sourceUpdatePersonalities, "personalities", "", "update personality registry path override")
	sourceUpdateCmd.Flags().StringVar(&sourceUpdatePlugins, "plugins", "", "update plugin registry path override")

	sourceCmd.AddCommand(sourceListCmd)
	sourceCmd.AddCommand(sourceAddCmd)
	sourceCmd.AddCommand(sourceUpdateCmd)
	sourceCmd.AddCommand(sourceRemoveCmd)
	sourceCmd.AddCommand(sourceSetDefaultCmd)
	sourceCmd.AddCommand(sourceShowCmd)
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
// Otherwise the resolver uses the default source only (for pull/create).
func buildSourceResolver(sourceFilter string) (*config.SourceResolver, error) {
	sc, err := loadSourceConfig()
	if err != nil {
		return nil, err
	}
	resolver := config.NewSourceResolver(sc.Sources)
	if sourceFilter != "" {
		return resolver.ForSource(sourceFilter)
	}
	return resolver.DefaultOnly(), nil
}

// buildListSourceResolver creates a SourceResolver for list commands.
// --all returns all sources, --source filters to one, default shows only the default source.
func buildListSourceResolver(sourceFilter string, all bool) (*config.SourceResolver, error) {
	if sourceFilter != "" && all {
		return nil, fmt.Errorf("--source and --all are mutually exclusive")
	}
	sc, err := loadSourceConfig()
	if err != nil {
		return nil, err
	}
	resolver := config.NewSourceResolver(sc.Sources)
	if sourceFilter != "" {
		return resolver.ForSource(sourceFilter)
	}
	if all {
		return resolver, nil
	}
	return resolver.DefaultOnly(), nil
}

func runSourceList(cmd *cobra.Command, _ []string) error {
	sc, err := loadSourceConfig()
	if err != nil {
		return err
	}

	customCount := 0
	for _, s := range sc.Sources {
		if s.Name != config.DefaultSourceName {
			customCount++
		}
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
	if err := w.Flush(); err != nil {
		return err
	}

	if customCount == 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "No custom sources configured. Use 'klausctl source add' to register one.")
	}

	return nil
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

func runSourceUpdate(cmd *cobra.Command, args []string) error {
	sc, err := loadSourceConfig()
	if err != nil {
		return err
	}

	patch := config.Source{
		Registry:      sourceUpdateRegistry,
		Toolchains:    sourceUpdateToolchains,
		Personalities: sourceUpdatePersonalities,
		Plugins:       sourceUpdatePlugins,
	}

	if err := sc.Update(args[0], patch); err != nil {
		return err
	}

	if err := sc.Save(); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Updated source %q\n", args[0])
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

func runSourceShow(cmd *cobra.Command, args []string) error {
	sc, err := loadSourceConfig()
	if err != nil {
		return err
	}

	s := sc.Get(args[0])
	if s == nil {
		return fmt.Errorf("source %q not found", args[0])
	}

	out := cmd.OutOrStdout()
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Name:\t%s\n", s.Name)
	fmt.Fprintf(w, "Registry:\t%s\n", s.Registry)
	if s.Default {
		fmt.Fprintf(w, "Default:\tyes\n")
	}
	fmt.Fprintf(w, "Toolchains:\t%s\n", s.ToolchainRegistry())
	fmt.Fprintf(w, "Personalities:\t%s\n", s.PersonalityRegistry())
	fmt.Fprintf(w, "Plugins:\t%s\n", s.PluginRegistry())
	return w.Flush()
}
