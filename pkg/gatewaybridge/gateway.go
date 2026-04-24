package gatewaybridge

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/giantswarm/klausctl/pkg/config"
)

const (
	// klausGatewayBinEnv names the env var that overrides the klaus-gateway
	// host binary path.
	klausGatewayBinEnv = "KLAUS_GATEWAY_BIN"
)

// startKlausGateway spawns the klaus-gateway process in host-binary mode
// when KLAUS_GATEWAY_BIN (or --klaus-gateway-bin) resolves, and in container
// mode otherwise. Returns the in-progress status, already health-checked.
func startKlausGateway(ctx context.Context, paths *config.Paths, opts Options, cfg gatewayConfig, agentGatewayURL string) (*Status, error) {
	port := firstNonZero(opts.Port, cfg.Port, DefaultKlausGatewayPort)

	bin := opts.KlausGatewayBin
	if bin == "" {
		bin = os.Getenv(klausGatewayBinEnv)
	}

	mode := "host"
	if bin == "" {
		// Fall back to a binary on PATH; if still missing, we cannot start
		// container mode from this PR (see docs/gatewaybridge.md) and must
		// error loudly.
		if p, err := exec.LookPath("klaus-gateway"); err == nil {
			bin = p
		}
	}

	if bin == "" {
		return nil, fmt.Errorf("klaus-gateway not found: set %s or --klaus-gateway-bin, or install klaus-gateway on PATH (container mode is not yet available in this release)", klausGatewayBinEnv)
	}

	logLevel := opts.LogLevel
	if logLevel == "" {
		logLevel = cfg.LogLevel
	}
	if logLevel == "" {
		logLevel = "info"
	}

	selfBin, err := os.Executable()
	if err != nil {
		// Fall back to the command name if the executable path is unavailable
		// (e.g. in a static go test binary). klaus-gateway will still be able
		// to find klausctl on PATH in most dev environments.
		selfBin = "klausctl"
	}

	args := []string{
		fmt.Sprintf("--listen-address=:%d", port),
		fmt.Sprintf("--admin-address=:%d", port+1),
		"--store=bolt",
		"--bolt-path", paths.GatewayRoutesBoltFile,
		"--driver=klausctl",
		"--klausctl-bin", selfBin,
		fmt.Sprintf("--log-level=%s", logLevel),
	}
	if agentGatewayURL != "" {
		args = append(args, fmt.Sprintf("--agentgateway-url=%s", agentGatewayURL))
	}

	cmd := exec.CommandContext(ctx, bin, args...) // #nosec G204,G702 -- bridge subprocess with controlled args
	setSysProcAttr(cmd)
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting klaus-gateway: %w", err)
	}

	pid := cmd.Process.Pid
	if err := writePID(paths.KlausGatewayPIDFile, pid); err != nil {
		return nil, fmt.Errorf("writing klaus-gateway PID file: %w", err)
	}
	if err := writePort(paths.KlausGatewayPortFile, port); err != nil {
		return nil, fmt.Errorf("writing klaus-gateway port file: %w", err)
	}
	if err := writeMode(paths.KlausGatewayPIDFile, mode); err != nil {
		return nil, fmt.Errorf("writing klaus-gateway mode file: %w", err)
	}

	if err := cmd.Process.Release(); err != nil {
		return nil, fmt.Errorf("releasing klaus-gateway process: %w", err)
	}

	if err := waitHealthy(ctx, port); err != nil {
		return nil, fmt.Errorf("klaus-gateway started (PID %d) but did not become healthy: %w", pid, err)
	}

	return &Status{
		Running: true,
		PID:     pid,
		Port:    port,
		URL:     bridgeURL(port),
		Healthy: true,
		Mode:    mode,
	}, nil
}

// stopKlausGateway terminates the klaus-gateway process and removes its
// PID/port files. Returns nil when no PID file is present (idempotent).
func stopKlausGateway(paths *config.Paths) error {
	pid, err := readPID(paths.KlausGatewayPIDFile)
	if err != nil {
		return nil
	}
	if err := stopProcess(pid); err != nil {
		return fmt.Errorf("stopping klaus-gateway: %w", err)
	}
	cleanupKlausGatewayFiles(paths)
	return nil
}

// firstNonZero returns the first non-zero int from the argument list, or zero
// if they are all zero.
func firstNonZero(vs ...int) int {
	for _, v := range vs {
		if v != 0 {
			return v
		}
	}
	return 0
}

// modePath returns the path where the start mode ("host" or "container") is
// persisted alongside the PID file.
func modePath(pidFile string) string {
	return strings.TrimSuffix(pidFile, ".pid") + ".mode"
}

func writeMode(pidFile, mode string) error {
	return os.WriteFile(modePath(pidFile), []byte(mode), 0o600)
}

func modeLabel(pidFile string) string {
	data, err := os.ReadFile(modePath(pidFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
