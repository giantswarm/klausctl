package gatewaybridge

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/mcpserverstore"
)

func testPaths(t *testing.T) *config.Paths {
	t.Helper()
	dir := t.TempDir()
	gatewayDir := filepath.Join(dir, "gateway")
	if err := os.MkdirAll(gatewayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return &config.Paths{
		ConfigDir:               dir,
		McpServersFile:          filepath.Join(dir, "mcpservers.yaml"),
		GatewayConfigDir:        gatewayDir,
		GatewayConfigFile:       filepath.Join(gatewayDir, "config.yaml"),
		GatewayRoutesBoltFile:   filepath.Join(gatewayDir, "routes.bolt"),
		GatewaySlackSecretsFile: filepath.Join(gatewayDir, "slack-secrets.yaml"),
		KlausGatewayPIDFile:     filepath.Join(dir, "klaus-gateway.pid"),
		KlausGatewayPortFile:    filepath.Join(dir, "klaus-gateway.port"),
		AgentGatewayPIDFile:     filepath.Join(dir, "agentgateway.pid"),
		AgentGatewayPortFile:    filepath.Join(dir, "agentgateway.port"),
	}
}

func TestBridgeURL(t *testing.T) {
	got := bridgeURL(8080)
	want := "http://host.docker.internal:8080/mcp"
	if got != want {
		t.Errorf("bridgeURL(8080) = %q, want %q", got, want)
	}
}

func TestGetStatus_NotRunning(t *testing.T) {
	paths := testPaths(t)
	st := GetStatus(paths)
	if st.Running {
		t.Error("expected Running=false when no PID file is present")
	}
}

func TestGetStatus_StalePID(t *testing.T) {
	paths := testPaths(t)
	if err := os.WriteFile(paths.KlausGatewayPIDFile, []byte("999999999"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := GetStatus(paths)
	if st.Running {
		t.Error("expected Running=false for stale PID")
	}
	if _, err := os.Stat(paths.KlausGatewayPIDFile); !os.IsNotExist(err) {
		t.Error("expected stale klaus-gateway PID file to be cleaned up")
	}
}

func TestRegisterBridge(t *testing.T) {
	paths := testPaths(t)
	if err := registerBridge(paths, 8080); err != nil {
		t.Fatalf("registerBridge: %v", err)
	}
	store, err := mcpserverstore.Load(paths.McpServersFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	def, err := store.Get(BridgeName)
	if err != nil {
		t.Fatalf("Get(%q): %v", BridgeName, err)
	}
	want := "http://host.docker.internal:8080/mcp"
	if def.URL != want {
		t.Errorf("URL = %q, want %q", def.URL, want)
	}
}

func TestRegisterBridge_Idempotent(t *testing.T) {
	paths := testPaths(t)
	for i := 0; i < 3; i++ {
		if err := registerBridge(paths, 8080); err != nil {
			t.Fatalf("registerBridge (call %d): %v", i+1, err)
		}
	}
	store, err := mcpserverstore.Load(paths.McpServersFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	names := store.List()
	if len(names) != 1 {
		t.Errorf("expected 1 entry, got %d: %v", len(names), names)
	}
}

func TestRegisterBridge_UpdatesPort(t *testing.T) {
	paths := testPaths(t)
	if err := registerBridge(paths, 8080); err != nil {
		t.Fatalf("registerBridge(8080): %v", err)
	}
	if err := registerBridge(paths, 9999); err != nil {
		t.Fatalf("registerBridge(9999): %v", err)
	}
	store, err := mcpserverstore.Load(paths.McpServersFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	def, err := store.Get(BridgeName)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := "http://host.docker.internal:9999/mcp"
	if def.URL != want {
		t.Errorf("URL = %q, want %q", def.URL, want)
	}
}

func TestDeregisterBridge_PreservesOthers(t *testing.T) {
	paths := testPaths(t)
	store, err := mcpserverstore.Load(paths.McpServersFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	store.Add("other", mcpserverstore.McpServerDef{URL: "https://other.example.com/mcp"})
	store.Add(BridgeName, mcpserverstore.McpServerDef{URL: bridgeURL(8080)})
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	deregisterBridge(paths)

	reloaded, err := mcpserverstore.Load(paths.McpServersFile)
	if err != nil {
		t.Fatalf("Load after deregister: %v", err)
	}
	if _, err := reloaded.Get("other"); err != nil {
		t.Error("unrelated MCP server entry should be preserved")
	}
	if _, err := reloaded.Get(BridgeName); err == nil {
		t.Error("klaus-gateway should be removed after deregisterBridge")
	}
}

func TestDeregisterBridge_NoEntry(t *testing.T) {
	paths := testPaths(t)
	deregisterBridge(paths) // must not panic or error
}

func TestLoadGatewayConfig_Missing(t *testing.T) {
	paths := testPaths(t)
	cfg := loadGatewayConfig(paths.GatewayConfigFile)
	if cfg.Port != 0 {
		t.Errorf("expected zero port for missing config, got %d", cfg.Port)
	}
	if len(cfg.EnabledAdapters()) != 0 {
		t.Errorf("expected no adapters for missing config, got %v", cfg.EnabledAdapters())
	}
}

func TestLoadGatewayConfig_Parses(t *testing.T) {
	paths := testPaths(t)
	content := []byte(`logLevel: debug
port: 9100
adapters:
  slack:
    enabled: true
  github:
    enabled: false
agentGateway:
  enabled: true
  port: 9200
`)
	if err := os.WriteFile(paths.GatewayConfigFile, content, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := loadGatewayConfig(paths.GatewayConfigFile)
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfg.LogLevel)
	}
	if cfg.Port != 9100 {
		t.Errorf("Port = %d, want 9100", cfg.Port)
	}
	if !cfg.AgentGateway.Enabled {
		t.Error("expected AgentGateway.Enabled=true")
	}
	if cfg.AgentGateway.Port != 9200 {
		t.Errorf("AgentGateway.Port = %d, want 9200", cfg.AgentGateway.Port)
	}
	got := cfg.EnabledAdapters()
	if len(got) != 1 || got[0] != "slack" {
		t.Errorf("EnabledAdapters = %v, want [slack]", got)
	}
}

func TestFirstNonZero(t *testing.T) {
	if v := firstNonZero(0, 0, 42, 7); v != 42 {
		t.Errorf("firstNonZero(0,0,42,7) = %d, want 42", v)
	}
	if v := firstNonZero(0, 0, 0); v != 0 {
		t.Errorf("firstNonZero(0,0,0) = %d, want 0", v)
	}
	if v := firstNonZero(); v != 0 {
		t.Errorf("firstNonZero() = %d, want 0", v)
	}
}

func TestStart_NoBinary(t *testing.T) {
	paths := testPaths(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv(klausGatewayBinEnv, "")
	_, err := Start(context.Background(), paths, Options{})
	if err == nil {
		t.Fatal("expected error when klaus-gateway binary is not available")
	}
}

func TestEnsureRunning_NoBinary(t *testing.T) {
	paths := testPaths(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv(klausGatewayBinEnv, "")
	_, err := EnsureRunning(context.Background(), paths, Options{})
	if err == nil {
		t.Fatal("expected error when klaus-gateway binary is not available")
	}
}

// TestOwnership asserts the strict file ownership boundary: klausctl writes
// to *.pid, *.port, routes.bolt; never to gateway/config.yaml or
// gateway/slack-secrets.yaml.
func TestOwnership(t *testing.T) {
	paths := testPaths(t)

	// Write klausctl-owned files via the helpers.
	if err := writePID(paths.KlausGatewayPIDFile, 1234); err != nil {
		t.Fatalf("writePID: %v", err)
	}
	if err := writePort(paths.KlausGatewayPortFile, 8080); err != nil {
		t.Fatalf("writePort: %v", err)
	}
	if err := writePID(paths.AgentGatewayPIDFile, 5678); err != nil {
		t.Fatalf("writePID agent: %v", err)
	}
	if err := writePort(paths.AgentGatewayPortFile, 8090); err != nil {
		t.Fatalf("writePort agent: %v", err)
	}

	// User-owned files are never touched by klausctl: create them by hand
	// and confirm cleanup does not remove them.
	if err := os.WriteFile(paths.GatewayConfigFile, []byte("logLevel: info\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.GatewaySlackSecretsFile, []byte("token: placeholder\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cleanupKlausGatewayFiles(paths)
	cleanupAgentGatewayFiles(paths)

	// klausctl-owned files should be gone.
	for _, p := range []string{
		paths.KlausGatewayPIDFile,
		paths.KlausGatewayPortFile,
		paths.AgentGatewayPIDFile,
		paths.AgentGatewayPortFile,
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected klausctl-owned file %s removed, err=%v", p, err)
		}
	}

	// User-owned files must be untouched.
	if _, err := os.Stat(paths.GatewayConfigFile); err != nil {
		t.Errorf("user-owned gateway/config.yaml should be preserved: %v", err)
	}
	if _, err := os.Stat(paths.GatewaySlackSecretsFile); err != nil {
		t.Errorf("user-owned gateway/slack-secrets.yaml should be preserved: %v", err)
	}
}

func TestStop_NoPIDFile(t *testing.T) {
	paths := testPaths(t)
	// No PID files exist -- Stop is idempotent and returns nil.
	if err := Stop(context.Background(), paths); err != nil {
		t.Errorf("Stop with no PID files returned error: %v", err)
	}
}

func TestStop_CleansStaleFiles(t *testing.T) {
	paths := testPaths(t)

	// Simulate stale state: PID files point at non-existent PIDs.
	if err := writePID(paths.KlausGatewayPIDFile, 999999999); err != nil {
		t.Fatal(err)
	}
	if err := writePort(paths.KlausGatewayPortFile, 8080); err != nil {
		t.Fatal(err)
	}
	// Register an mcpservers.yaml entry to confirm deregistration.
	if err := registerBridge(paths, 8080); err != nil {
		t.Fatalf("registerBridge: %v", err)
	}

	if err := Stop(context.Background(), paths); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if _, err := os.Stat(paths.KlausGatewayPIDFile); !os.IsNotExist(err) {
		t.Error("expected klaus-gateway PID file removed after Stop")
	}
	if _, err := os.Stat(paths.KlausGatewayPortFile); !os.IsNotExist(err) {
		t.Error("expected klaus-gateway port file removed after Stop")
	}
	store, err := mcpserverstore.Load(paths.McpServersFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := store.Get(BridgeName); err == nil {
		t.Error("expected klaus-gateway removed from mcpservers.yaml after Stop")
	}
}
