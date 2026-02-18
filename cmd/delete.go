package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

var deleteYes bool

var deleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete an instance",
	Args:  cobra.ExactArgs(1),
	RunE:  runDelete,
}

func init() {
	deleteCmd.Flags().BoolVar(&deleteYes, "yes", false, "skip confirmation prompt")
	rootCmd.AddCommand(deleteCmd)
}

func runDelete(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	name := args[0]
	if err := config.ValidateInstanceName(name); err != nil {
		return err
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := config.MigrateLayout(paths); err != nil {
		return err
	}
	paths = paths.ForInstance(name)

	if _, err := os.Stat(paths.InstanceDir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("instance %q does not exist", name)
		}
		return err
	}

	if !deleteYes {
		if err := confirmDelete(cmd, name); err != nil {
			return err
		}
	}

	inst, _ := instance.Load(paths)
	if err := cleanupInstanceContainer(ctx, name, inst); err != nil {
		return err
	}

	if err := os.RemoveAll(paths.InstanceDir); err != nil {
		return fmt.Errorf("deleting instance directory: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Deleted instance %q.\n", name)
	return nil
}

func confirmDelete(cmd *cobra.Command, name string) error {
	fmt.Fprintf(cmd.OutOrStdout(), "Delete instance %q? [y/N]: ", name)
	reader := bufio.NewReader(cmd.InOrStdin())
	answer, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		return fmt.Errorf("delete cancelled")
	}
	return nil
}

func cleanupInstanceContainer(ctx context.Context, instanceName string, inst *instance.Instance) error {
	containerName := instance.ContainerName(instanceName)

	runtimeCandidates := []string{}
	if inst != nil {
		if inst.Name != "" {
			containerName = inst.ContainerName()
		}
		if inst.Runtime != "" {
			runtimeCandidates = append(runtimeCandidates, inst.Runtime)
		}
	}
	for _, rtName := range []string{"docker", "podman"} {
		if !slices.Contains(runtimeCandidates, rtName) {
			runtimeCandidates = append(runtimeCandidates, rtName)
		}
	}

	for _, rtName := range runtimeCandidates {
		rt, err := runtime.New(rtName)
		if err != nil {
			continue
		}
		if err := stopAndRemoveContainerIfExists(ctx, rt, containerName); err != nil {
			return fmt.Errorf("cleaning container %s via %s: %w", containerName, rtName, err)
		}
	}

	return nil
}

func stopAndRemoveContainerIfExists(ctx context.Context, rt runtime.Runtime, containerName string) error {
	status, err := rt.Status(ctx, containerName)
	if err != nil || status == "" {
		return nil
	}

	if status == "running" {
		if err := rt.Stop(ctx, containerName); err != nil {
			return fmt.Errorf("stopping container: %w", err)
		}
	}
	if err := rt.Remove(ctx, containerName); err != nil {
		return fmt.Errorf("removing container: %w", err)
	}
	return nil
}
