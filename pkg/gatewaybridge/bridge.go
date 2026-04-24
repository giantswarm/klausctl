// Package gatewaybridge manages the local lifecycle of the klaus-gateway
// service (and optionally the upstream agentgateway it can run behind).
// It mirrors the shape of pkg/musterbridge: klausctl spawns the processes,
// tracks their PID/port, registers the HTTP MCP endpoint into mcpservers.yaml,
// and auto-starts on `klaus create` when a personality/instance declares
// `requires.gateway: true`.
package gatewaybridge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/mcpserverstore"
)

const (
	// BridgeName is the name used when registering klaus-gateway in the
	// MCP server store.
	BridgeName = "klaus-gateway"

	// DefaultKlausGatewayPort is the default port for klaus-gateway.
	DefaultKlausGatewayPort = 8080
	// DefaultAgentGatewayPort is the default port for the optional agentgateway.
	DefaultAgentGatewayPort = 8090
)

// Options configures the bridge lifecycle. All fields are optional; zero
// values fall back to defaults or values read from gateway/config.yaml.
type Options struct {
	// WithAgentGateway, when true, also starts the agentgateway process
	// before starting klaus-gateway.
	WithAgentGateway bool

	// Port overrides the klaus-gateway listen port.
	Port int

	// AgentGatewayPort overrides the agentgateway listen port.
	AgentGatewayPort int

	// KlausGatewayBin overrides the klaus-gateway host binary path.
	// If empty, the KLAUS_GATEWAY_BIN env var is consulted, then PATH.
	KlausGatewayBin string

	// AgentGatewayBin overrides the agentgateway host binary path.
	// If empty, the KLAUS_AGENTGATEWAY_BIN env var is consulted, then PATH.
	AgentGatewayBin string

	// LogLevel overrides the gateway log level (defaults to "info").
	LogLevel string
}

// Status represents the state of the gateway bridge.
type Status struct {
	Running          bool               `json:"running"`
	PID              int                `json:"pid,omitempty"`
	Port             int                `json:"port,omitempty"`
	URL              string             `json:"url,omitempty"`
	Healthy          bool               `json:"healthy"`
	Mode             string             `json:"mode,omitempty"`
	Adapters         []string           `json:"adapters,omitempty"`
	WithAgentGateway bool               `json:"withAgentGateway,omitempty"`
	AgentGateway     *AgentGatewayState `json:"agentGateway,omitempty"`
}

// AgentGatewayState describes the agentgateway process state as a sub-status.
type AgentGatewayState struct {
	Running bool   `json:"running"`
	PID     int    `json:"pid,omitempty"`
	Port    int    `json:"port,omitempty"`
	URL     string `json:"url,omitempty"`
	Healthy bool   `json:"healthy"`
	Mode    string `json:"mode,omitempty"`
}

// Start launches the klaus-gateway process (and optionally agentgateway) in
// the background. It is idempotent: if klaus-gateway is already running and
// healthy, it returns the existing status.
func Start(ctx context.Context, paths *config.Paths, opts Options) (*Status, error) {
	if st, alive := checkRunning(paths, opts); alive {
		if err := registerBridge(paths, st.Port); err != nil {
			return nil, fmt.Errorf("registering klaus-gateway in mcpserverstore: %w", err)
		}
		return st, nil
	}

	cfg := loadGatewayConfig(paths.GatewayConfigFile)

	// Ensure the routes.bolt parent directory exists (klausctl-owned).
	if err := os.MkdirAll(paths.GatewayConfigDir, 0o750); err != nil {
		return nil, fmt.Errorf("creating gateway config dir: %w", err)
	}

	withAgentGW := opts.WithAgentGateway || cfg.AgentGateway.Enabled

	var agentStatus *AgentGatewayState
	agentGatewayURL := ""
	if withAgentGW {
		st, err := startAgentGateway(ctx, paths, opts, cfg)
		if err != nil {
			return nil, err
		}
		agentStatus = st
		agentGatewayURL = fmt.Sprintf("http://localhost:%d", st.Port)
	}

	gwStatus, err := startKlausGateway(ctx, paths, opts, cfg, agentGatewayURL)
	if err != nil {
		// Roll back agentgateway if we started it in this call.
		if withAgentGW {
			_ = stopAgentGateway(paths)
		}
		return nil, err
	}

	if err := registerBridge(paths, gwStatus.Port); err != nil {
		return nil, fmt.Errorf("registering klaus-gateway in mcpserverstore: %w", err)
	}

	gwStatus.WithAgentGateway = withAgentGW
	gwStatus.AgentGateway = agentStatus
	gwStatus.Adapters = cfg.EnabledAdapters()
	return gwStatus, nil
}

// Stop terminates klaus-gateway first, then agentgateway if running, cleans
// up PID/port files, and removes the klaus-gateway entry from mcpservers.yaml.
func Stop(ctx context.Context, paths *config.Paths) error {
	_ = ctx // reserved for future cancellation support

	gwErr := stopKlausGateway(paths)
	agErr := stopAgentGateway(paths)

	deregisterBridge(paths)

	if gwErr != nil {
		return gwErr
	}
	return agErr
}

// GetStatus returns the current bridge status without mutating any state.
func GetStatus(paths *config.Paths) *Status {
	st, alive := checkRunning(paths, Options{})
	if !alive {
		return &Status{Running: false}
	}
	return st
}

// EnsureRunning starts the bridge if it is not already running, otherwise
// ensures the bridge entry exists in the MCP server store. Used by the
// auto-start integration in the instance start flow.
func EnsureRunning(ctx context.Context, paths *config.Paths, opts Options) (*Status, error) {
	if st, alive := checkRunning(paths, opts); alive {
		if err := registerBridge(paths, st.Port); err != nil {
			return nil, fmt.Errorf("registering klaus-gateway in mcpserverstore: %w", err)
		}
		return st, nil
	}
	return Start(ctx, paths, opts)
}

// Restart stops and re-starts the bridge. Useful after config changes.
func Restart(ctx context.Context, paths *config.Paths, opts Options) (*Status, error) {
	_ = Stop(ctx, paths)
	return Start(ctx, paths, opts)
}

// checkRunning returns the current status iff klaus-gateway's PID file points
// at a live process. Stale PID files are cleaned up as a side effect.
func checkRunning(paths *config.Paths, opts Options) (*Status, bool) {
	pid, err := readPID(paths.KlausGatewayPIDFile)
	if err != nil {
		return nil, false
	}
	if !processAlive(pid) {
		cleanupKlausGatewayFiles(paths)
		return nil, false
	}

	port, err := readPort(paths.KlausGatewayPortFile)
	if err != nil {
		port = DefaultKlausGatewayPort
	}

	cfg := loadGatewayConfig(paths.GatewayConfigFile)

	st := &Status{
		Running:  true,
		PID:      pid,
		Port:     port,
		URL:      bridgeURL(port),
		Healthy:  healthyNow(port),
		Mode:     modeLabel(paths.KlausGatewayPIDFile),
		Adapters: cfg.EnabledAdapters(),
	}

	// Also attach agentgateway state when present.
	if ag, ok := checkAgentGatewayRunning(paths); ok {
		st.WithAgentGateway = true
		st.AgentGateway = ag
	} else if opts.WithAgentGateway {
		st.WithAgentGateway = true
	}

	return st, true
}

// checkAgentGatewayRunning reports the current agentgateway sub-status.
func checkAgentGatewayRunning(paths *config.Paths) (*AgentGatewayState, bool) {
	pid, err := readPID(paths.AgentGatewayPIDFile)
	if err != nil {
		return nil, false
	}
	if !processAlive(pid) {
		cleanupAgentGatewayFiles(paths)
		return nil, false
	}
	port, err := readPort(paths.AgentGatewayPortFile)
	if err != nil {
		port = DefaultAgentGatewayPort
	}
	return &AgentGatewayState{
		Running: true,
		PID:     pid,
		Port:    port,
		URL:     fmt.Sprintf("http://localhost:%d", port),
		Healthy: healthyNow(port),
		Mode:    modeLabel(paths.AgentGatewayPIDFile),
	}, true
}

// bridgeURL is the URL the klaus-gateway MCP endpoint is registered under.
// Matches the musterbridge convention so containers can reach it via
// host.docker.internal.
func bridgeURL(port int) string {
	return fmt.Sprintf("http://host.docker.internal:%d/mcp", port)
}

func registerBridge(paths *config.Paths, port int) error {
	store, err := mcpserverstore.Load(paths.McpServersFile)
	if err != nil {
		return err
	}
	store.Add(BridgeName, mcpserverstore.McpServerDef{URL: bridgeURL(port)})
	return store.Save()
}

func deregisterBridge(paths *config.Paths) {
	store, err := mcpserverstore.Load(paths.McpServersFile)
	if err != nil {
		return
	}
	if err := store.Remove(BridgeName); err != nil {
		return
	}
	_ = store.Save()
}

func cleanupKlausGatewayFiles(paths *config.Paths) {
	_ = os.Remove(paths.KlausGatewayPIDFile)
	_ = os.Remove(paths.KlausGatewayPortFile)
	_ = os.Remove(modePath(paths.KlausGatewayPIDFile))
}

func cleanupAgentGatewayFiles(paths *config.Paths) {
	_ = os.Remove(paths.AgentGatewayPIDFile)
	_ = os.Remove(paths.AgentGatewayPortFile)
	_ = os.Remove(modePath(paths.AgentGatewayPIDFile))
}

// waitForExit polls until the process exits or the deadline passes. Returns
// true if the process has exited.
func waitForExit(pid int, deadline time.Duration) bool {
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if !processAlive(pid) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// stopProcess signals proc SIGTERM, waits up to 5s, then SIGKILLs if needed.
func stopProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	if err := sendTermSignal(proc); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return fmt.Errorf("SIGTERM pid %d: %w", pid, err)
	}
	if !waitForExit(pid, 5*time.Second) {
		_ = sendKillSignal(proc)
	}
	return nil
}
