package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/musterbridge"
)

var musterCmd = &cobra.Command{
	Use:   "muster",
	Short: "Manage the muster MCP bridge",
	Long: `Manage the shared muster process that bridges stdio MCP servers to HTTP
for use by containerized klaus agents.

The bridge reads MCP server definitions from ~/.config/klausctl/muster/mcpservers/
and exposes them as a single HTTP endpoint that containers can reach.`,
}

var musterStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the muster bridge",
	Long: `Start a background muster process that aggregates configured MCP servers
and exposes them via HTTP. The bridge is registered as "muster-bridge" in the
managed MCP server store so instances can reference it via --mcpserver.`,
	Args: cobra.NoArgs,
	RunE: runMusterStart,
}

var musterStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the muster bridge",
	Args:  cobra.NoArgs,
	RunE:  runMusterStop,
}

var musterStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show muster bridge status",
	Args:  cobra.NoArgs,
	RunE:  runMusterStatus,
}

var musterRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the muster bridge to pick up config changes",
	Args:  cobra.NoArgs,
	RunE:  runMusterRestart,
}

func init() {
	musterCmd.AddCommand(musterStartCmd)
	musterCmd.AddCommand(musterStopCmd)
	musterCmd.AddCommand(musterStatusCmd)
	musterCmd.AddCommand(musterRestartCmd)
	rootCmd.AddCommand(musterCmd)
}

func runMusterStart(cmd *cobra.Command, _ []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	out := cmd.OutOrStdout()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	fmt.Fprintln(out, "Starting muster bridge...")
	st, err := musterbridge.Start(ctx, paths)
	if err != nil {
		return err
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, green("Muster bridge running."))
	printBridgeStatus(out, st)
	return nil
}

func runMusterStop(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	if err := musterbridge.Stop(paths); err != nil {
		return err
	}

	fmt.Fprintln(out, green("Muster bridge stopped."))
	return nil
}

func runMusterStatus(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	st := musterbridge.GetStatus(paths)
	if !st.Running {
		fmt.Fprintln(out, "Muster bridge: not running")
		return nil
	}

	fmt.Fprintln(out, "Muster bridge:", green("running"))
	printBridgeStatus(out, st)
	return nil
}

func runMusterRestart(cmd *cobra.Command, _ []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	out := cmd.OutOrStdout()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	fmt.Fprintln(out, "Restarting muster bridge...")
	st, err := musterbridge.Restart(ctx, paths)
	if err != nil {
		return err
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, green("Muster bridge running."))
	printBridgeStatus(out, st)
	return nil
}

func printBridgeStatus(out io.Writer, st *musterbridge.Status) {
	fmt.Fprintf(out, "  PID:  %d\n", st.PID)
	fmt.Fprintf(out, "  Port: %d\n", st.Port)
	fmt.Fprintf(out, "  URL:  %s\n", st.URL)
	if len(st.MCPServers) > 0 {
		fmt.Fprintln(out, "  Managed MCP servers:")
		for _, s := range st.MCPServers {
			fmt.Fprintf(out, "    - %s\n", s.Name)
		}
	}
}
