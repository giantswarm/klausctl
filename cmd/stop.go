package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

var stopCmd = &cobra.Command{
	Use:   "stop [name]",
	Short: "Stop the running klaus instance",
	Long:  `Stop and remove the running klaus container.`,
	Args:  cobra.MaximumNArgs(1),
	RunE:  runStop,
}

var stopAll bool

func init() {
	stopCmd.Flags().BoolVar(&stopAll, "all", false, "stop all instances")
	rootCmd.AddCommand(stopCmd)
}

func runStop(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	out := cmd.OutOrStdout()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := config.MigrateLayout(paths); err != nil {
		return fmt.Errorf("migrating config layout: %w", err)
	}

	if stopAll && len(args) > 0 {
		return fmt.Errorf("--all cannot be used with an instance name")
	}

	if stopAll {
		return stopAllInstances(ctx, out, paths)
	}

	instanceName, err := resolveOptionalInstanceName(args, "stop", cmd.ErrOrStderr())
	if err != nil {
		return err
	}

	paths = paths.ForInstance(instanceName)

	inst, err := instance.Load(paths)
	if err != nil {
		// No instance state -- nothing to stop. Idempotent success.
		fmt.Fprintf(out, "No klaus instance running for %q.\n", instanceName)
		return nil
	}

	rt, err := runtime.New(inst.Runtime)
	if err != nil {
		return err
	}

	containerName := inst.ContainerName()

	// Check current status.
	status, err := rt.Status(ctx, containerName)
	if err != nil || status == "" {
		fmt.Fprintf(out, "Container %s does not exist.\n", containerName)
		_ = instance.Clear(paths)
		return nil
	}

	// Stop the container if running.
	if status == "running" {
		fmt.Fprintf(out, "Stopping %s...\n", containerName)
		if err := rt.Stop(ctx, containerName); err != nil {
			return fmt.Errorf("stopping container: %w", err)
		}
	}

	// Remove the container.
	fmt.Fprintf(out, "Removing %s...\n", containerName)
	if err := rt.Remove(ctx, containerName); err != nil {
		return fmt.Errorf("removing container: %w", err)
	}

	// Clear instance state.
	if err := instance.Clear(paths); err != nil {
		return fmt.Errorf("clearing instance state: %w", err)
	}

	fmt.Fprintln(out, green("Klaus instance stopped."))
	return nil
}

func stopAllInstances(ctx context.Context, out io.Writer, paths *config.Paths) error {
	instances, err := instance.LoadAll(paths)
	if err != nil {
		return err
	}
	if len(instances) == 0 {
		fmt.Fprintln(out, "No klaus instances running.")
		return nil
	}

	sort.Slice(instances, func(i, j int) bool {
		return instances[i].Name < instances[j].Name
	})

	for _, inst := range instances {
		rt, err := runtime.New(inst.Runtime)
		if err != nil {
			return err
		}
		name := inst.ContainerName()
		status, err := rt.Status(ctx, name)
		if err != nil || status == "" {
			_ = instance.Clear(paths.ForInstance(inst.Name))
			continue
		}
		if status == "running" {
			fmt.Fprintf(out, "Stopping %s...\n", name)
			if err := rt.Stop(ctx, name); err != nil {
				return fmt.Errorf("stopping %s: %w", name, err)
			}
		}
		fmt.Fprintf(out, "Removing %s...\n", name)
		if err := rt.Remove(ctx, name); err != nil {
			return fmt.Errorf("removing %s: %w", name, err)
		}
		if err := instance.Clear(paths.ForInstance(inst.Name)); err != nil {
			return fmt.Errorf("clearing state for %s: %w", inst.Name, err)
		}
	}

	fmt.Fprintln(out, green("All klaus instances stopped."))
	return nil
}
