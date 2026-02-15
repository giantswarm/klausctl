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
	"github.com/giantswarm/klausctl/pkg/runtime"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show instance status",
	Long:  `Show the status of the running klaus instance.`,
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, _ []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	out := cmd.OutOrStdout()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	inst, err := instance.Load(paths)
	if err != nil {
		fmt.Fprintln(out, "No klaus instance found.")
		fmt.Fprintln(out, "Run 'klausctl start' to start one.")
		return nil
	}

	rt, err := runtime.New(inst.Runtime)
	if err != nil {
		return err
	}

	containerName := inst.ContainerName()

	// Get container status.
	status, err := rt.Status(ctx, containerName)
	if err != nil || status == "" {
		fmt.Fprintf(out, "Instance:   %s\n", inst.Name)
		fmt.Fprintf(out, "Status:     not found (stale state)\n")
		fmt.Fprintf(out, "\nThe container no longer exists. Run 'klausctl start' to start a new one.\n")
		return nil
	}

	fmt.Fprintf(out, "Instance:   %s\n", inst.Name)
	fmt.Fprintf(out, "Status:     %s\n", status)
	fmt.Fprintf(out, "Container:  %s\n", containerName)
	fmt.Fprintf(out, "Runtime:    %s\n", inst.Runtime)
	fmt.Fprintf(out, "Image:      %s\n", inst.Image)
	fmt.Fprintf(out, "Workspace:  %s\n", inst.Workspace)

	if status == "running" {
		fmt.Fprintf(out, "MCP:        http://localhost:%d\n", inst.Port)

		// Try to get detailed info from the runtime.
		info, inspectErr := rt.Inspect(ctx, containerName)
		if inspectErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not inspect container: %v\n", inspectErr)
		}

		switch {
		case inspectErr == nil && !info.StartedAt.IsZero():
			fmt.Fprintf(out, "Uptime:     %s\n", formatDuration(time.Since(info.StartedAt)))
		case !inst.StartedAt.IsZero():
			fmt.Fprintf(out, "Uptime:     %s\n", formatDuration(time.Since(inst.StartedAt)))
		}
	}

	return nil
}

// formatDuration formats a duration in a human-readable way.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%dd%dh", days, hours)
}
