package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running klaus instance",
	Long:  `Stop and remove the running klaus container.`,
	RunE:  runStop,
}

func init() {
	rootCmd.AddCommand(stopCmd)
}

func runStop(_ *cobra.Command, _ []string) error {
	inst, err := instance.Load()
	if err != nil {
		return err
	}

	rt, err := runtime.New(inst.Runtime)
	if err != nil {
		return err
	}

	containerName := inst.ContainerName()

	// Check current status.
	status, err := rt.Status(containerName)
	if err != nil || status == "" {
		fmt.Printf("Container %s does not exist.\n", containerName)
		_ = instance.Clear()
		return nil
	}

	// Stop the container if running.
	if status == "running" {
		fmt.Printf("Stopping %s...\n", containerName)
		if err := rt.Stop(containerName); err != nil {
			return fmt.Errorf("stopping container: %w", err)
		}
	}

	// Remove the container.
	fmt.Printf("Removing %s...\n", containerName)
	if err := rt.Remove(containerName); err != nil {
		return fmt.Errorf("removing container: %w", err)
	}

	// Clear instance state.
	if err := instance.Clear(); err != nil {
		return fmt.Errorf("clearing instance state: %w", err)
	}

	fmt.Println("Klaus instance stopped.")
	return nil
}
