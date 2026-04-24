package cmd

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/archive"
	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/mcpclient"
	"github.com/giantswarm/klausctl/pkg/runtime"
	"github.com/giantswarm/klausctl/pkg/worktree"
)

var (
	deleteYes       bool
	deleteNoArchive bool
)

var deleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete an instance",
	Args:  cobra.ExactArgs(1),
	RunE:  runDelete,
}

func init() {
	deleteCmd.Flags().BoolVar(&deleteYes, "yes", false, "skip confirmation prompt")
	deleteCmd.Flags().BoolVar(&deleteNoArchive, "no-archive", false, "skip archiving the agent transcript before deleting")
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

	// Archive transcript before deleting if the container is still running.
	inst, _ := instance.Load(paths)
	if inst != nil && !deleteNoArchive && !archive.Exists(paths.ArchivesDir, inst.UUID) {
		archiveBeforeDelete(ctx, inst, paths)
	}

	// Load instance config to check for workspace clone before removing anything.
	cfg, _ := config.Load(paths.ConfigFile)
	if cfg != nil && cfg.WorktreePath != "" {
		if err := worktree.Remove(cfg.Workspace, cfg.WorktreePath); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to remove workspace clone: %v\n", err)
		}
	}

	if err := cleanupInstanceContainer(ctx, name, inst); err != nil {
		return err
	}

	if err := os.RemoveAll(paths.InstanceDir); err != nil {
		return fmt.Errorf("deleting instance directory: %w", err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Deleted instance %q.\n", name)
	return nil
}

func confirmDelete(cmd *cobra.Command, name string) error {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Delete instance %q? [y/N]: ", name)
	reader := bufio.NewReader(cmd.InOrStdin())
	answer, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" { //nolint:goconst
		return fmt.Errorf("delete cancelled")
	}
	return nil
}

// archiveBeforeDelete captures the agent transcript before deletion.
// Best-effort: logs and continues on failure.
func archiveBeforeDelete(ctx context.Context, inst *instance.Instance, paths *config.Paths) {
	client := mcpclient.New(buildVersion)
	defer client.Close()

	if err := archive.Capture(ctx, client, inst, paths.ArchivesDir); err != nil {
		log.Printf("Warning: failed to archive %q before delete: %v", inst.Name, err)
	}
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

	if status == "running" { //nolint:goconst
		if err := rt.Stop(ctx, containerName); err != nil {
			return fmt.Errorf("stopping container: %w", err)
		}
	}
	if err := rt.Remove(ctx, containerName); err != nil {
		return fmt.Errorf("removing container: %w", err)
	}
	return nil
}
