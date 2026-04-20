package gatewaybridge

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/giantswarm/klausctl/pkg/config"
)

const (
	// agentGatewayBinEnv names the env var that overrides the agentgateway
	// host binary path.
	agentGatewayBinEnv = "KLAUS_AGENTGATEWAY_BIN"
)

// startAgentGateway spawns the optional agentgateway process. It is started
// before klaus-gateway so klaus-gateway can point at it via --agentgateway-url.
func startAgentGateway(ctx context.Context, paths *config.Paths, opts Options, cfg gatewayConfig) (*AgentGatewayState, error) {
	port := firstNonZero(opts.AgentGatewayPort, cfg.AgentGateway.Port, DefaultAgentGatewayPort)

	bin := opts.AgentGatewayBin
	if bin == "" {
		bin = os.Getenv(agentGatewayBinEnv)
	}
	if bin == "" {
		if p, err := exec.LookPath("agentgateway"); err == nil {
			bin = p
		}
	}
	if bin == "" {
		return nil, fmt.Errorf("agentgateway not found: set %s or --agentgateway-bin, or install agentgateway on PATH", agentGatewayBinEnv)
	}

	args := []string{
		fmt.Sprintf("--listen-address=:%d", port),
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	setSysProcAttr(cmd)
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting agentgateway: %w", err)
	}

	pid := cmd.Process.Pid
	if err := writePID(paths.AgentGatewayPIDFile, pid); err != nil {
		return nil, fmt.Errorf("writing agentgateway PID file: %w", err)
	}
	if err := writePort(paths.AgentGatewayPortFile, port); err != nil {
		return nil, fmt.Errorf("writing agentgateway port file: %w", err)
	}
	if err := writeMode(paths.AgentGatewayPIDFile, "host"); err != nil {
		return nil, fmt.Errorf("writing agentgateway mode file: %w", err)
	}

	if err := cmd.Process.Release(); err != nil {
		return nil, fmt.Errorf("releasing agentgateway process: %w", err)
	}

	if err := waitHealthy(ctx, port); err != nil {
		return nil, fmt.Errorf("agentgateway started (PID %d) but did not become healthy: %w", pid, err)
	}

	return &AgentGatewayState{
		Running: true,
		PID:     pid,
		Port:    port,
		URL:     fmt.Sprintf("http://localhost:%d", port),
		Healthy: true,
		Mode:    "host",
	}, nil
}

// stopAgentGateway terminates the agentgateway process and removes its
// PID/port files. Returns nil when no PID file is present (idempotent).
func stopAgentGateway(paths *config.Paths) error {
	pid, err := readPID(paths.AgentGatewayPIDFile)
	if err != nil {
		return nil
	}
	if err := stopProcess(pid); err != nil {
		return fmt.Errorf("stopping agentgateway: %w", err)
	}
	cleanupAgentGatewayFiles(paths)
	return nil
}
