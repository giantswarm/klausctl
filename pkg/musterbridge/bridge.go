// Package musterbridge manages the lifecycle of a shared local muster process
// that aggregates stdio MCP servers behind an HTTP endpoint. Containers reach
// muster via host.docker.internal, giving agents access to the same MCP tools
// the user has in their IDE.
package musterbridge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/mcpserverstore"
)

const (
	DefaultPort       = 8090
	BridgeName        = "muster"
	healthPollTimeout = 15 * time.Second
	healthPollDelay   = 500 * time.Millisecond
)

// musterConfig represents the subset of muster's config.yaml that we read.
type musterConfig struct {
	Aggregator struct {
		Port int `yaml:"port"`
	} `yaml:"aggregator"`
}

// Status represents the state of the muster bridge.
type Status struct {
	Running    bool             `json:"running"`
	PID        int              `json:"pid,omitempty"`
	Port       int              `json:"port,omitempty"`
	URL        string           `json:"url,omitempty"`
	MCPServers []MCPServerEntry `json:"mcpServers,omitempty"`
}

// MCPServerEntry describes an MCP server file found in the muster config directory.
type MCPServerEntry struct {
	Name string `json:"name"`
	File string `json:"file"`
}

// resolvePort reads the configured port from muster's config.yaml or returns
// the default. The resolved port is also persisted so other commands can read it.
func resolvePort(paths *config.Paths) int {
	cfgPath := filepath.Join(paths.MusterConfigDir, "config.yaml")
	data, err := os.ReadFile(cfgPath) // #nosec G304 -- user-supplied or trusted local path; not exposed to untrusted input
	if err != nil {
		return DefaultPort
	}
	var mc musterConfig
	if err := yaml.Unmarshal(data, &mc); err != nil || mc.Aggregator.Port == 0 {
		return DefaultPort
	}
	return mc.Aggregator.Port
}

// Start launches a muster serve process in the background. It is idempotent:
// if muster is already running and healthy, it returns nil.
func Start(ctx context.Context, paths *config.Paths) (*Status, error) {
	hasCfg, err := paths.HasMusterConfig()
	if err != nil {
		return nil, fmt.Errorf("checking muster config: %w", err)
	}
	if !hasCfg {
		return nil, fmt.Errorf("no MCP server files in %s; create at least one .yaml file before starting the bridge", paths.MusterMCPServersDir)
	}

	musterBin, err := findMuster()
	if err != nil {
		return nil, err
	}

	// If already running, ensure the store entry exists and return current status.
	if st, alive := checkRunning(paths); alive {
		if err := registerBridge(paths, st.Port); err != nil {
			return nil, fmt.Errorf("registering muster in mcpserverstore: %w", err)
		}
		return st, nil
	}

	port := resolvePort(paths)

	cmd := exec.CommandContext(ctx, musterBin, "serve", // #nosec G204 -- container runtime CLI invocation with controlled args
		"--config-path", paths.MusterConfigDir,
		"--silent",
	)
	setSysProcAttr(cmd)
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting muster: %w", err)
	}

	pid := cmd.Process.Pid
	if err := os.WriteFile(paths.MusterPIDFile, []byte(strconv.Itoa(pid)), 0o600); err != nil {
		return nil, fmt.Errorf("writing PID file: %w", err)
	}
	if err := os.WriteFile(paths.MusterPortFile, []byte(strconv.Itoa(port)), 0o600); err != nil {
		return nil, fmt.Errorf("writing port file: %w", err)
	}

	// Detach from the child so we don't become its reaper.
	if err := cmd.Process.Release(); err != nil {
		return nil, fmt.Errorf("releasing muster process: %w", err)
	}

	// Wait for muster to become healthy.
	if err := waitHealthy(ctx, port); err != nil {
		return nil, fmt.Errorf("muster started (PID %d) but did not become healthy: %w", pid, err)
	}

	// Auto-register in mcpserverstore.
	if err := registerBridge(paths, port); err != nil {
		return nil, fmt.Errorf("registering muster in mcpserverstore: %w", err)
	}

	return &Status{
		Running:    true,
		PID:        pid,
		Port:       port,
		URL:        bridgeURL(port),
		MCPServers: listMCPServerFiles(paths),
	}, nil
}

// Stop terminates the running muster process and cleans up state files.
func Stop(paths *config.Paths) error {
	pid, err := readPID(paths.MusterPIDFile)
	if err != nil {
		return fmt.Errorf("muster bridge is not running (no PID file)")
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		cleanupFiles(paths)
		return nil
	}

	if err := sendTermSignal(proc); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			cleanupFiles(paths)
			return nil
		}
		return fmt.Errorf("sending SIGTERM to muster (PID %d): %w", pid, err)
	}

	// Wait briefly for the process to exit.
	done := make(chan struct{})
	go func() {
		_, _ = proc.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = sendKillSignal(proc)
	}

	cleanupFiles(paths)
	deregisterBridge(paths)
	return nil
}

// GetStatus returns the current muster bridge status without modifying anything.
func GetStatus(paths *config.Paths) *Status {
	st, alive := checkRunning(paths)
	if !alive {
		return &Status{Running: false}
	}
	return st
}

// EnsureRunning starts the muster bridge if it is not already running and
// guarantees the bridge entry exists in the MCP server store. Used by the
// auto-start integration in the instance start flow.
func EnsureRunning(ctx context.Context, paths *config.Paths) error {
	if st, alive := checkRunning(paths); alive {
		return registerBridge(paths, st.Port)
	}
	_, err := Start(ctx, paths)
	return err
}

// Restart stops and re-starts the muster bridge. Useful after config changes.
func Restart(ctx context.Context, paths *config.Paths) (*Status, error) {
	_ = Stop(paths)
	return Start(ctx, paths)
}

func checkRunning(paths *config.Paths) (*Status, bool) {
	pid, err := readPID(paths.MusterPIDFile)
	if err != nil {
		return nil, false
	}

	if !processAlive(pid) {
		cleanupFiles(paths)
		return nil, false
	}

	port, err := readPort(paths.MusterPortFile)
	if err != nil {
		port = DefaultPort
	}

	return &Status{
		Running:    true,
		PID:        pid,
		Port:       port,
		URL:        bridgeURL(port),
		MCPServers: listMCPServerFiles(paths),
	}, true
}

func findMuster() (string, error) {
	path, err := exec.LookPath("muster")
	if err != nil {
		return "", fmt.Errorf("muster binary not found in PATH; install it with: go install github.com/giantswarm/muster@latest")
	}
	return path, nil
}

func readPID(path string) (int, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- user-supplied or trusted local path; not exposed to untrusted input
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func readPort(path string) (int, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- user-supplied or trusted local path; not exposed to untrusted input
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func cleanupFiles(paths *config.Paths) {
	_ = os.Remove(paths.MusterPIDFile)
	_ = os.Remove(paths.MusterPortFile)
}

func waitHealthy(ctx context.Context, port int) error {
	deadline := time.Now().Add(healthPollTimeout)
	url := fmt.Sprintf("http://localhost:%d/mcp", port)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}

		time.Sleep(healthPollDelay)
	}

	return fmt.Errorf("timed out waiting for muster to respond on port %d", port)
}

func bridgeURL(port int) string {
	return fmt.Sprintf("http://host.docker.internal:%d/mcp", port)
}

func registerBridge(paths *config.Paths, port int) error {
	store, err := mcpserverstore.Load(paths.McpServersFile)
	if err != nil {
		return err
	}
	store.Add(BridgeName, mcpserverstore.McpServerDef{
		URL: bridgeURL(port),
	})
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

func listMCPServerFiles(paths *config.Paths) []MCPServerEntry {
	entries, err := os.ReadDir(paths.MusterMCPServersDir)
	if err != nil {
		return nil
	}
	var servers []MCPServerEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := filepath.Ext(name)
		if ext == ".yaml" || ext == ".yml" {
			servers = append(servers, MCPServerEntry{
				Name: strings.TrimSuffix(name, ext),
				File: name,
			})
		}
	}
	return servers
}
