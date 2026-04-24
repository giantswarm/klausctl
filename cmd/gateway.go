package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/internal/gatewaysurface"
	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/gatewaybridge"
)

var (
	gatewayStartWithAgentGateway bool
	gatewayStartPort             int
	gatewayStartAgentPort        int
	gatewayStartKlausGatewayBin  string
	gatewayStartAgentGatewayBin  string
	gatewayStartLogLevel         string

	gatewayStatusOutput string
)

var gatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "Manage the klaus-gateway bridge",
	Long: `Manage the local klaus-gateway service (and optionally the upstream
agentgateway it can run behind).

The bridge owns the process lifecycle, tracks PID/port in ~/.config/klausctl/,
health-checks /healthz, and registers klaus-gateway in mcpservers.yaml so
containerized klaus instances can reach it via host.docker.internal.`,
}

var gatewayStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the klaus-gateway bridge",
	Args:  cobra.NoArgs,
	RunE:  runGatewayStart,
}

var gatewayStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the klaus-gateway bridge",
	Args:  cobra.NoArgs,
	RunE:  runGatewayStop,
}

var gatewayStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show klaus-gateway bridge status",
	Args:  cobra.NoArgs,
	RunE:  runGatewayStatus,
}

func init() {
	// Every flag declared in gatewaysurface.StartFlags must have a matching
	// binding below. The parity test enforces that both surfaces line up.
	startFlags := gatewayStartCmd.Flags()
	startFlags.BoolVar(&gatewayStartWithAgentGateway, startFlagName("with-agentgateway"), false, startFlagDesc("with-agentgateway"))
	startFlags.IntVar(&gatewayStartPort, startFlagName("port"), 0, startFlagDesc("port"))
	startFlags.IntVar(&gatewayStartAgentPort, startFlagName("agentgateway-port"), 0, startFlagDesc("agentgateway-port"))
	startFlags.StringVar(&gatewayStartKlausGatewayBin, startFlagName("klaus-gateway-bin"), "", startFlagDesc("klaus-gateway-bin"))
	startFlags.StringVar(&gatewayStartAgentGatewayBin, startFlagName("agentgateway-bin"), "", startFlagDesc("agentgateway-bin"))
	startFlags.StringVar(&gatewayStartLogLevel, startFlagName("log-level"), "", startFlagDesc("log-level"))

	gatewayStatusCmd.Flags().StringVarP(&gatewayStatusOutput, statusFlagName("output"), "o", "text", statusFlagDesc("output"))

	gatewayCmd.AddCommand(gatewayStartCmd)
	gatewayCmd.AddCommand(gatewayStopCmd)
	gatewayCmd.AddCommand(gatewayStatusCmd)
	rootCmd.AddCommand(gatewayCmd)
}

// startFlagName returns the canonical CLI flag name for a key declared in
// gatewaysurface.StartFlags; panics on unknown key so misspellings fail at
// init time.
func startFlagName(key string) string {
	for _, f := range gatewaysurface.StartFlags {
		if f.CLIFlag == key {
			return f.CLIFlag
		}
	}
	panic("unknown gateway start flag: " + key)
}

func startFlagDesc(key string) string {
	for _, f := range gatewaysurface.StartFlags {
		if f.CLIFlag == key {
			return f.Description
		}
	}
	panic("unknown gateway start flag: " + key)
}

func statusFlagName(key string) string {
	for _, f := range gatewaysurface.StatusFlags {
		if f.CLIFlag == key {
			return f.CLIFlag
		}
	}
	panic("unknown gateway status flag: " + key)
}

func statusFlagDesc(key string) string {
	for _, f := range gatewaysurface.StatusFlags {
		if f.CLIFlag == key {
			return f.Description
		}
	}
	panic("unknown gateway status flag: " + key)
}

func runGatewayStart(cmd *cobra.Command, _ []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	out := cmd.OutOrStdout()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(out, "Starting klaus-gateway bridge...")
	st, err := gatewaybridge.Start(ctx, paths, gatewaybridge.Options{
		WithAgentGateway: gatewayStartWithAgentGateway,
		Port:             gatewayStartPort,
		AgentGatewayPort: gatewayStartAgentPort,
		KlausGatewayBin:  gatewayStartKlausGatewayBin,
		AgentGatewayBin:  gatewayStartAgentGatewayBin,
		LogLevel:         gatewayStartLogLevel,
	})
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, green("klaus-gateway bridge running."))
	printGatewayStatus(out, st)
	return nil
}

func runGatewayStop(cmd *cobra.Command, _ []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	out := cmd.OutOrStdout()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	if err := gatewaybridge.Stop(ctx, paths); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(out, green("klaus-gateway bridge stopped."))
	return nil
}

func runGatewayStatus(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}

	st := gatewaybridge.GetStatus(paths)

	if strings.EqualFold(gatewayStatusOutput, "json") {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(st)
	}

	if !st.Running {
		_, _ = fmt.Fprintln(out, "klaus-gateway bridge: not running")
		return nil
	}
	_, _ = fmt.Fprintln(out, "klaus-gateway bridge:", green("running"))
	printGatewayStatus(out, st)
	return nil
}

func printGatewayStatus(out io.Writer, st *gatewaybridge.Status) {
	_, _ = fmt.Fprintf(out, "  PID:      %d\n", st.PID)
	_, _ = fmt.Fprintf(out, "  Port:     %d\n", st.Port)
	_, _ = fmt.Fprintf(out, "  URL:      %s\n", st.URL)
	if st.Mode != "" {
		_, _ = fmt.Fprintf(out, "  Mode:     %s\n", st.Mode)
	}
	healthLabel := "yes" //nolint:goconst
	if !st.Healthy {
		healthLabel = yellow("no")
	}
	_, _ = fmt.Fprintf(out, "  Healthy:  %s\n", healthLabel)
	if len(st.Adapters) > 0 {
		_, _ = fmt.Fprintf(out, "  Adapters: %s\n", strings.Join(st.Adapters, ", "))
	}
	if st.WithAgentGateway && st.AgentGateway != nil {
		_, _ = fmt.Fprintln(out, "  agentgateway:")
		_, _ = fmt.Fprintf(out, "    PID:     %d\n", st.AgentGateway.PID)
		_, _ = fmt.Fprintf(out, "    Port:    %d\n", st.AgentGateway.Port)
		_, _ = fmt.Fprintf(out, "    URL:     %s\n", st.AgentGateway.URL)
		aghealth := "yes"
		if !st.AgentGateway.Healthy {
			aghealth = yellow("no")
		}
		_, _ = fmt.Fprintf(out, "    Healthy: %s\n", aghealth)
	}
}
