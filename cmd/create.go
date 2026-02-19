package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/oci"
)

var (
	createPersonality string
	createToolchain   string
	createPlugins     []string
	createPort        int
)

var createCmd = &cobra.Command{
	Use:   "create <name> [workspace]",
	Short: "Create and start a named klaus instance",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runCreate,
}

func init() {
	createCmd.Flags().StringVar(&createPersonality, "personality", "", "personality short name or OCI reference")
	createCmd.Flags().StringVar(&createToolchain, "toolchain", "", "toolchain short name or OCI reference")
	createCmd.Flags().StringSliceVar(&createPlugins, "plugin", nil, "additional plugin short name or OCI reference (repeatable)")
	createCmd.Flags().IntVar(&createPort, "port", 0, "override auto-selected port")
	rootCmd.AddCommand(createCmd)
}

func runCreate(cmd *cobra.Command, args []string) error {
	instanceName := args[0]
	if err := config.ValidateInstanceName(instanceName); err != nil {
		return err
	}

	workspace := ""
	if len(args) > 1 {
		workspace = args[1]
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("determining current directory: %w", err)
		}
		workspace = cwd
	}

	ctx := context.Background()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := config.MigrateLayout(paths); err != nil {
		return fmt.Errorf("migrating config layout: %w", err)
	}

	instancePaths := paths.ForInstance(instanceName)
	if _, err := os.Stat(instancePaths.InstanceDir); err == nil {
		return fmt.Errorf("instance %q already exists", instanceName)
	}

	personality, toolchain, plugins, err := resolveCreateRefs(ctx, createPersonality, createToolchain, createPlugins)
	if err != nil {
		return err
	}

	cfg, err := config.GenerateInstanceConfig(paths, config.CreateOptions{
		Name:        instanceName,
		Workspace:   workspace,
		Personality: personality,
		Toolchain:   toolchain,
		Plugins:     plugins,
		Port:        createPort,
		Context:     ctx,
		Output:      cmd.OutOrStdout(),
		ResolvePersonality: func(ctx context.Context, ref string, outWriter io.Writer) (*config.ResolvedPersonality, error) {
			if err := config.EnsureDir(paths.PersonalitiesDir); err != nil {
				return nil, fmt.Errorf("creating personalities directory: %w", err)
			}
			pr, err := oci.ResolvePersonality(ctx, ref, paths.PersonalitiesDir, outWriter)
			if err != nil {
				return nil, err
			}

			plugins, err := oci.ResolvePluginRefs(ctx, pluginRefsFromSpec(pr.Spec.Plugins))
			if err != nil {
				return nil, fmt.Errorf("resolving personality plugins: %w", err)
			}

			return &config.ResolvedPersonality{
				Plugins: plugins,
				Image:   pr.Spec.Image,
			}, nil
		},
	})
	if err != nil {
		return err
	}

	if err := config.EnsureDir(instancePaths.InstanceDir); err != nil {
		return fmt.Errorf("creating instance directory: %w", err)
	}
	data, err := cfg.Marshal()
	if err != nil {
		return fmt.Errorf("serializing config: %w", err)
	}
	if err := os.WriteFile(instancePaths.ConfigFile, data, 0o644); err != nil {
		return fmt.Errorf("writing instance config: %w", err)
	}

	// Ensure rendered output stays under the instance directory.
	if err := config.EnsureDir(filepath.Dir(instancePaths.RenderedDir)); err != nil {
		return fmt.Errorf("creating rendered directory parent: %w", err)
	}

	return startInstance(cmd, instanceName, "", instancePaths.ConfigFile)
}

// resolveCreateRefs resolves personality, toolchain, and plugin short names
// to full OCI references with proper semver tags from the registry.
func resolveCreateRefs(ctx context.Context, personality, toolchain string, plugins []string) (string, string, []string, error) {
	if personality != "" {
		ref, err := oci.ResolveArtifactRef(ctx, personality, oci.DefaultPersonalityRegistry, "")
		if err != nil {
			return "", "", nil, fmt.Errorf("resolving personality: %w", err)
		}
		personality = ref
	}

	if toolchain != "" {
		ref, err := oci.ResolveArtifactRef(ctx, toolchain, oci.DefaultToolchainRegistry, "klaus-")
		if err != nil {
			return "", "", nil, fmt.Errorf("resolving toolchain: %w", err)
		}
		toolchain = ref
	}

	resolved := make([]string, 0, len(plugins))
	for _, p := range plugins {
		ref, err := oci.ResolveArtifactRef(ctx, p, oci.DefaultPluginRegistry, "")
		if err != nil {
			return "", "", nil, fmt.Errorf("resolving plugin: %w", err)
		}
		resolved = append(resolved, ref)
	}

	return personality, toolchain, resolved, nil
}

// pluginRefsFromSpec converts personality spec plugin references to config.Plugin entries.
func pluginRefsFromSpec(refs []oci.PluginReference) []config.Plugin {
	plugins := make([]config.Plugin, 0, len(refs))
	for _, p := range refs {
		plugins = append(plugins, oci.PluginFromReference(p))
	}
	return plugins
}
