package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/oci"
	"github.com/giantswarm/klausctl/pkg/orchestrator"
	"github.com/giantswarm/klausctl/pkg/renderer"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

var startWorkspace string

var startCmd = &cobra.Command{
	Use:   "start [name]",
	Short: "Start a local klaus instance",
	Long: `Start a local klaus container with the configured settings.

This command:
  1. Loads configuration from ~/.config/klausctl/instances/<name>/config.yaml
  2. Resolves personality (if configured): pulls the OCI artifact, merges
     plugins, applies image override, and prepares SOUL.md
  3. Pulls OCI plugins (personality + instance-level)
  4. Renders configuration files (skills, settings, MCP config)
  5. Starts a container with the correct env vars, mounts, and ports`,
	Args: cobra.MaximumNArgs(1),
	RunE: runStart,
}

func init() {
	startCmd.Flags().StringVar(&startWorkspace, "workspace", "", "workspace directory to mount (overrides config file)")
	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	instanceName, err := resolveOptionalInstanceName(args, "start", cmd.ErrOrStderr())
	if err != nil {
		return err
	}

	configPathOverride := ""
	if cfgFile != "" {
		configPathOverride = cfgFile
	}
	return startInstance(cmd, instanceName, startWorkspace, configPathOverride)
}

func startInstance(cmd *cobra.Command, instanceName, workspaceOverride, configPathOverride string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := config.MigrateLayout(paths); err != nil {
		return fmt.Errorf("migrating config layout: %w", err)
	}
	paths = paths.ForInstance(instanceName)

	cfgPath := paths.ConfigFile
	if configPathOverride != "" {
		cfgPath = configPathOverride
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	applyWorkspaceOverride(cfg, workspaceOverride)

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

	// Derive the instance name and container name consistently.
	containerName := instance.ContainerName(instanceName)

	// Check if already running.
	inst, err := instance.Load(paths)
	if err == nil && inst.Name != "" {
		status, sErr := rt.Status(ctx, inst.ContainerName())
		if sErr == nil && status == "running" {
			return fmt.Errorf(
				"instance %q is already running (container: %s, MCP: http://localhost:%d)\nUse 'klausctl stop %s' to stop it first",
				inst.Name, inst.ContainerName(), inst.Port,
				inst.Name,
			)
		}
		// Clean up stale container.
		_ = rt.Remove(ctx, inst.ContainerName())
		_ = instance.Clear(paths)
	}

	// Resolve personality if configured. This pulls the personality artifact,
	// merges its plugins with the user's, and optionally overrides the image.
	var personalityDir string
	if cfg.Personality != "" {
		fmt.Fprintln(out, "Resolving personality...")

		// Resolve :latest / empty tags to actual semver before pulling.
		// Plugins and toolchain don't need resolution here -- plugins are
		// resolved inside PullPlugins, and the toolchain image tag is used
		// as-is by the container runtime.
		resolvedRef, err := oci.ResolveArtifactRef(ctx, cfg.Personality, oci.DefaultPersonalityRegistry, "")
		if err != nil {
			return fmt.Errorf("resolving personality ref: %w", err)
		}
		cfg.Personality = resolvedRef

		if err := config.EnsureDir(paths.PersonalitiesDir); err != nil {
			return fmt.Errorf("creating personalities directory: %w", err)
		}

		pr, err := oci.ResolvePersonality(ctx, cfg.Personality, paths.PersonalitiesDir, out)
		if err != nil {
			return fmt.Errorf("resolving personality: %w", err)
		}
		personalityDir = pr.Dir

		// Merge personality plugins with user plugins (user wins on conflict).
		cfg.Plugins = oci.MergePlugins(pr.Spec.Plugins, cfg.Plugins)

		// Use personality image if the user didn't explicitly set one.
		if !cfg.ImageExplicitlySet() && pr.Spec.Image != "" {
			cfg.Image = pr.Spec.Image
		}
	}

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
	runOpts, err := orchestrator.BuildRunOptions(cfg, paths, containerName, image, personalityDir)
	if err != nil {
		return fmt.Errorf("building run options: %w", err)
	}

	// Pull the image with streamed progress. If the pull fails but the
	// image is already cached locally (e.g. expired registry credentials),
	// continue with the cached copy.
	fmt.Fprintf(out, "Pulling %s...\n", image)
	if err := rt.Pull(ctx, image, out); err != nil {
		images, imgErr := rt.Images(ctx, image)
		if imgErr != nil || len(images) == 0 {
			return fmt.Errorf("pulling image: %w", err)
		}
		_, _ = fmt.Fprintln(out, "Pull failed, using locally cached image.")
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
		Personality: cfg.Personality,
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
	fmt.Fprintf(out, "  Instance:    %s\n", inst.Name)
	if cfg.Personality != "" {
		fmt.Fprintf(out, "  Personality: %s\n", cfg.Personality)
	}
	fmt.Fprintf(out, "  Container:   %s\n", containerName)
	fmt.Fprintf(out, "  Image:       %s\n", image)
	fmt.Fprintf(out, "  Workspace:   %s\n", inst.Workspace)
	fmt.Fprintf(out, "  MCP:         http://localhost:%d\n", cfg.Port)

	// Warn about missing API key after the success context so it doesn't
	// appear before the user knows what's happening.
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		fmt.Fprintf(errOut, "\n%s ANTHROPIC_API_KEY is not set; the claude agent may fail to authenticate.\n", yellow("Warning:"))
	}

	fmt.Fprintf(out, "\nUse 'klausctl logs %s' to view output, 'klausctl stop %s' to stop.\n", inst.Name, inst.Name)
	return nil
}

func applyWorkspaceOverride(cfg *config.Config, workspaceOverride string) {
	if workspaceOverride != "" {
		cfg.Workspace = workspaceOverride
	}
}
