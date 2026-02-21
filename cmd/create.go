package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/oci"
)

var (
	createPersonality  string
	createToolchain    string
	createPlugins      []string
	createPort         int
	createEnv          []string
	createEnvForward   []string
	createPermMode     string
	createModel        string
	createSystemPrompt string
	createMaxBudget    float64
)

var createCmd = &cobra.Command{
	Use:   "create <name> [workspace]",
	Short: "Create and start a named klaus instance",
	Long: `Create and start a named klaus instance.

Override flags (--env, --env-forward, --permission-mode, --model, etc.) are
applied on top of any values defined by the resolved personality. Map-like
fields (envVars, envForward) are merged; scalar fields (model, permissionMode,
systemPrompt, maxBudget) replace the personality default.

MCP server configurations can be supplied via the MCP tool interface
(mcpServers parameter) or by editing the instance config file directly.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runCreate,
}

func init() {
	createCmd.Flags().StringVar(&createPersonality, "personality", "", "personality short name or OCI reference")
	createCmd.Flags().StringVar(&createToolchain, "toolchain", "", "toolchain short name or OCI reference")
	createCmd.Flags().StringSliceVar(&createPlugins, "plugin", nil, "additional plugin short name or OCI reference (repeatable)")
	createCmd.Flags().IntVar(&createPort, "port", 0, "override auto-selected port")
	createCmd.Flags().StringArrayVar(&createEnv, "env", nil, "environment variable KEY=VALUE (repeatable)")
	createCmd.Flags().StringArrayVar(&createEnvForward, "env-forward", nil, "host environment variable name to forward (repeatable)")
	createCmd.Flags().StringVar(&createPermMode, "permission-mode", "", "Claude permission mode: default, acceptEdits, bypassPermissions, dontAsk, plan, delegate")
	createCmd.Flags().StringVar(&createModel, "model", "", "Claude model (e.g. sonnet, opus)")
	createCmd.Flags().StringVar(&createSystemPrompt, "system-prompt", "", "system prompt override for the Claude agent")
	createCmd.Flags().Float64Var(&createMaxBudget, "max-budget", 0, "maximum dollar budget per invocation (0 = no limit)")
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

	personality, toolchain, plugins, err := oci.ResolveCreateRefs(ctx, createPersonality, createToolchain, createPlugins)
	if err != nil {
		return err
	}

	envVars, err := parseEnvFlags(createEnv)
	if err != nil {
		return err
	}

	opts := config.CreateOptions{
		Name:           instanceName,
		Workspace:      workspace,
		Personality:    personality,
		Toolchain:      toolchain,
		Plugins:        plugins,
		Port:           createPort,
		EnvVars:        envVars,
		EnvForward:     createEnvForward,
		PermissionMode: createPermMode,
		Model:          createModel,
		SystemPrompt:   createSystemPrompt,
		Context:        ctx,
		Output:         cmd.OutOrStdout(),
		ResolvePersonality: func(ctx context.Context, ref string, outWriter io.Writer) (*config.ResolvedPersonality, error) {
			if err := config.EnsureDir(paths.PersonalitiesDir); err != nil {
				return nil, fmt.Errorf("creating personalities directory: %w", err)
			}
			pr, err := oci.ResolvePersonality(ctx, ref, paths.PersonalitiesDir, outWriter)
			if err != nil {
				return nil, err
			}

			plugins, err := oci.ResolvePluginRefs(ctx, pr.Spec.Plugins)
			if err != nil {
				return nil, fmt.Errorf("resolving personality plugins: %w", err)
			}

			client := oci.NewDefaultClient()
			image, err := client.ResolveToolchainRef(ctx, pr.Spec.Image)
			if err != nil {
				return nil, fmt.Errorf("resolving personality image: %w", err)
			}

			return &config.ResolvedPersonality{
				Plugins: plugins,
				Image:   image,
			}, nil
		},
	}
	if cmd.Flags().Changed("max-budget") {
		opts.MaxBudgetUSD = &createMaxBudget
	}

	cfg, err := config.GenerateInstanceConfig(paths, opts)
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

// parseEnvFlags parses KEY=VALUE pairs from --env flag values into a map.
func parseEnvFlags(envFlags []string) (map[string]string, error) {
	if len(envFlags) == 0 {
		return nil, nil
	}
	m := make(map[string]string, len(envFlags))
	for _, kv := range envFlags {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, fmt.Errorf("invalid --env value %q: expected KEY=VALUE format", kv)
		}
		m[k] = v
	}
	return m, nil
}
