package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

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

func runStatus(_ *cobra.Command, _ []string) error {
	inst, err := instance.Load()
	if err != nil {
		fmt.Println("No klaus instance found.")
		fmt.Println("Run 'klausctl start' to start one.")
		return nil
	}

	rt, err := runtime.New(inst.Runtime)
	if err != nil {
		return err
	}

	containerName := inst.ContainerName()

	// Get container status.
	status, err := rt.Status(containerName)
	if err != nil || status == "" {
		fmt.Printf("Instance:   %s\n", inst.Name)
		fmt.Printf("Status:     not found (stale state)\n")
		fmt.Printf("\nThe container no longer exists. Run 'klausctl start' to start a new one.\n")
		return nil
	}

	// Get detailed info.
	info, err := rt.Inspect(containerName)

	fmt.Printf("Instance:   %s\n", inst.Name)
	fmt.Printf("Status:     %s\n", status)
	fmt.Printf("Container:  %s\n", containerName)
	fmt.Printf("Runtime:    %s\n", inst.Runtime)
	fmt.Printf("Image:      %s\n", inst.Image)
	fmt.Printf("Workspace:  %s\n", inst.Workspace)

	if status == "running" {
		fmt.Printf("MCP:        http://localhost:%d\n", inst.Port)
		if err == nil && !info.StartedAt.IsZero() {
			fmt.Printf("Uptime:     %s\n", formatDuration(time.Since(info.StartedAt)))
		} else if !inst.StartedAt.IsZero() {
			fmt.Printf("Uptime:     %s\n", formatDuration(time.Since(inst.StartedAt)))
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
