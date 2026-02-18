package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

var statusOutput string

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show instance status",
	Long: `Show the status of the running klaus instance.

Returns exit code 1 when no instance is running, making it usable in scripts:

  if klausctl status >/dev/null 2>&1; then echo "running"; fi`,
	RunE: runStatus,
}

func init() {
	statusCmd.Flags().StringVarP(&statusOutput, "output", "o", "text", "output format: text, json")
	rootCmd.AddCommand(statusCmd)
}

// statusInfo holds all status fields for both text and JSON rendering.
type statusInfo struct {
	Instance    string `json:"instance"`
	Status      string `json:"status"`
	Personality string `json:"personality,omitempty"`
	Container   string `json:"container"`
	Runtime     string `json:"runtime"`
	Image       string `json:"image"`
	Workspace   string `json:"workspace"`
	MCP         string `json:"mcp,omitempty"`
	Uptime      string `json:"uptime,omitempty"`
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
		return fmt.Errorf("no klaus instance found; run 'klausctl start' to start one")
	}

	rt, err := runtime.New(inst.Runtime)
	if err != nil {
		return err
	}

	containerName := inst.ContainerName()

	// Get container status.
	status, err := rt.Status(ctx, containerName)
	if err != nil || status == "" {
		return fmt.Errorf("instance %q has stale state (container no longer exists); run 'klausctl start' to start a new one", inst.Name)
	}

	info := statusInfo{
		Instance:    inst.Name,
		Status:      status,
		Personality: inst.Personality,
		Container:   containerName,
		Runtime:     inst.Runtime,
		Image:       inst.Image,
		Workspace:   inst.Workspace,
	}

	if status == "running" {
		info.MCP = fmt.Sprintf("http://localhost:%d", inst.Port)

		// Try to get uptime from the runtime, fall back to saved state.
		cInfo, inspectErr := rt.Inspect(ctx, containerName)
		if inspectErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "%s could not inspect container: %v\n", yellow("Warning:"), inspectErr)
		}

		switch {
		case inspectErr == nil && !cInfo.StartedAt.IsZero():
			info.Uptime = formatDuration(time.Since(cInfo.StartedAt))
		case !inst.StartedAt.IsZero():
			info.Uptime = formatDuration(time.Since(inst.StartedAt))
		}
	}

	if statusOutput == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	// Text output.
	statusColor := status
	if status == "running" {
		statusColor = green(status)
	} else {
		statusColor = yellow(status)
	}

	fmt.Fprintf(out, "Instance:    %s\n", inst.Name)
	fmt.Fprintf(out, "Status:      %s\n", statusColor)
	if inst.Personality != "" {
		fmt.Fprintf(out, "Personality: %s\n", inst.Personality)
	}
	fmt.Fprintf(out, "Container:   %s\n", containerName)
	fmt.Fprintf(out, "Runtime:     %s\n", inst.Runtime)
	fmt.Fprintf(out, "Image:       %s\n", inst.Image)
	fmt.Fprintf(out, "Workspace:   %s\n", inst.Workspace)

	if status == "running" {
		fmt.Fprintf(out, "MCP:         %s\n", info.MCP)
		if info.Uptime != "" {
			fmt.Fprintf(out, "Uptime:      %s\n", info.Uptime)
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
