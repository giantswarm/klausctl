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
	Short: "Manage muster",
	Long: `Manage the shared muster process that exposes stdio MCP servers via HTTP
for use by containerized klaus agents.

Muster reads MCP server definitions from ~/.config/klausctl/muster/mcpservers/
and exposes them as a single HTTP endpoint that containers can reach.`,
}

var musterStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start muster",
	Long: `Start a background muster process that aggregates configured MCP servers
and exposes them via HTTP. Muster is registered as "muster" in the
managed MCP server store so instances can reference it via --mcpserver.`,
	Args: cobra.NoArgs,
	RunE: runMusterStart,
}

var musterStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop muster",
	Args:  cobra.NoArgs,
	RunE:  runMusterStop,
}

var musterStatusCmd = &cobra.Command{
	Use:   useStatus,
	Short: "Show muster status",
	Args:  cobra.NoArgs,
	RunE:  runMusterStatus,
}

var musterRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart muster to pick up config changes",
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

	_, _ = fmt.Fprintln(out, "Starting muster...")
	st, err := musterbridge.Start(ctx, paths)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, green("Muster running."))
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

	_, _ = fmt.Fprintln(out, green("Muster stopped."))
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
		_, _ = fmt.Fprintln(out, "Muster: not running")
		return nil
	}

	_, _ = fmt.Fprintln(out, "Muster:", green(statusRunning))
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

	_, _ = fmt.Fprintln(out, "Restarting muster...")
	st, err := musterbridge.Restart(ctx, paths)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, green("Muster running."))
	printBridgeStatus(out, st)
	return nil
}

func printBridgeStatus(out io.Writer, st *musterbridge.Status) {
	_, _ = fmt.Fprintf(out, "  PID:  %d\n", st.PID)
	_, _ = fmt.Fprintf(out, "  Port: %d\n", st.Port)
	_, _ = fmt.Fprintf(out, "  URL:  %s\n", st.URL)
	if len(st.MCPServers) > 0 {
		_, _ = fmt.Fprintln(out, "  Managed MCP servers:")
		for _, s := range st.MCPServers {
			_, _ = fmt.Fprintf(out, "    - %s\n", s.Name)
		}
	}
}
