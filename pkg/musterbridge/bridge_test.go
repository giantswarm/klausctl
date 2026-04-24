package musterbridge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/mcpserverstore"
)

func testPaths(t *testing.T) *config.Paths {
	t.Helper()
	dir := t.TempDir()
	musterDir := filepath.Join(dir, "muster")
	mcpServersDir := filepath.Join(musterDir, "mcpservers")
	if err := os.MkdirAll(mcpServersDir, 0o750); err != nil {
		t.Fatal(err)
	}
	return &config.Paths{
		ConfigDir:           dir,
		McpServersFile:      filepath.Join(dir, "mcpservers.yaml"),
		MusterConfigDir:     musterDir,
		MusterMCPServersDir: mcpServersDir,
		MusterPIDFile:       filepath.Join(dir, "muster.pid"),
		MusterPortFile:      filepath.Join(dir, "muster.port"),
	}
}

func TestResolvePort_Default(t *testing.T) {
	paths := testPaths(t)
	port := resolvePort(paths)
	if port != DefaultPort {
		t.Errorf("expected default port %d, got %d", DefaultPort, port)
	}
}

func TestResolvePort_FromConfig(t *testing.T) {
	paths := testPaths(t)
	cfgContent := []byte("aggregator:\n  port: 9999\n")
	if err := os.WriteFile(filepath.Join(paths.MusterConfigDir, "config.yaml"), cfgContent, 0o600); err != nil {
		t.Fatal(err)
	}
	port := resolvePort(paths)
	if port != 9999 {
		t.Errorf("expected port 9999, got %d", port)
	}
}

func TestGetStatus_NotRunning(t *testing.T) {
	paths := testPaths(t)
	st := GetStatus(paths)
	if st.Running {
		t.Error("expected not running")
	}
}

func TestGetStatus_StalePID(t *testing.T) {
	paths := testPaths(t)
	// Write a PID that doesn't exist.
	if err := os.WriteFile(paths.MusterPIDFile, []byte("999999999"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := GetStatus(paths)
	if st.Running {
		t.Error("expected not running for stale PID")
	}
	// PID file should be cleaned up.
	if _, err := os.Stat(paths.MusterPIDFile); !os.IsNotExist(err) {
		t.Error("expected PID file to be cleaned up")
	}
}

func TestListMCPServerFiles(t *testing.T) {
	paths := testPaths(t)

	for _, name := range []string{"pro.yaml", "klausctl.yml", "readme.txt"} {
		if err := os.WriteFile(filepath.Join(paths.MusterMCPServersDir, name), []byte(""), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	entries := listMCPServerFiles(paths)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
	}
	if !names["pro"] || !names["klausctl"] {
		t.Errorf("expected pro and klausctl, got %v", entries)
	}
}

func TestBridgeURL(t *testing.T) {
	url := bridgeURL(8090)
	expected := "http://host.docker.internal:8090/mcp"
	if url != expected {
		t.Errorf("expected %q, got %q", expected, url)
	}
}

func TestRegisterBridge(t *testing.T) {
	paths := testPaths(t)

	if err := registerBridge(paths, 8090); err != nil {
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
	want := "http://host.docker.internal:8090/mcp"
	if def.URL != want {
		t.Errorf("URL = %q, want %q", def.URL, want)
	}
}

func TestRegisterBridge_Idempotent(t *testing.T) {
	paths := testPaths(t)

	for i := 0; i < 3; i++ {
		if err := registerBridge(paths, 8090); err != nil {
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

	if err := registerBridge(paths, 8090); err != nil {
		t.Fatalf("registerBridge(8090): %v", err)
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

func TestDeregisterBridge(t *testing.T) {
	paths := testPaths(t)

	if err := registerBridge(paths, 8090); err != nil {
		t.Fatalf("registerBridge: %v", err)
	}

	deregisterBridge(paths)

	store, err := mcpserverstore.Load(paths.McpServersFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := store.Get(BridgeName); err == nil {
		t.Error("expected bridge entry to be removed after deregisterBridge")
	}
}

func TestDeregisterBridge_NoEntry(t *testing.T) {
	paths := testPaths(t)
	// Should not panic or error when entry doesn't exist.
	deregisterBridge(paths)
}

func TestDeregisterBridge_PreservesOtherEntries(t *testing.T) {
	paths := testPaths(t)

	store, err := mcpserverstore.Load(paths.McpServersFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	store.Add("other-server", mcpserverstore.McpServerDef{URL: "https://other.example.com/mcp"})
	store.Add(BridgeName, mcpserverstore.McpServerDef{URL: bridgeURL(8090)})
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	deregisterBridge(paths)

	reloaded, err := mcpserverstore.Load(paths.McpServersFile)
	if err != nil {
		t.Fatalf("Load after deregister: %v", err)
	}
	if _, err := reloaded.Get("other-server"); err != nil {
		t.Error("other-server should be preserved after deregisterBridge")
	}
	if _, err := reloaded.Get(BridgeName); err == nil {
		t.Error("muster should be removed after deregisterBridge")
	}
}

func TestStart_NoConfig(t *testing.T) {
	dir := t.TempDir()
	paths := &config.Paths{
		ConfigDir:           dir,
		McpServersFile:      filepath.Join(dir, "mcpservers.yaml"),
		MusterConfigDir:     filepath.Join(dir, "muster"),
		MusterMCPServersDir: filepath.Join(dir, "muster", "mcpservers"),
		MusterPIDFile:       filepath.Join(dir, "muster.pid"),
		MusterPortFile:      filepath.Join(dir, "muster.port"),
	}

	_, err := Start(t.Context(), paths)
	if err == nil {
		t.Fatal("expected error when no config exists")
	}
}

func TestStart_NoMusterBinary(t *testing.T) {
	paths := testPaths(t)
	// Create a YAML file so HasMusterConfig passes.
	if err := os.WriteFile(filepath.Join(paths.MusterMCPServersDir, "test.yaml"), []byte("kind: MCPServer"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Override PATH to ensure muster is not found.
	t.Setenv("PATH", t.TempDir())
	_, err := Start(t.Context(), paths)
	if err == nil {
		t.Fatal("expected error when muster binary not found")
	}
}

func TestStop_NotRunning(t *testing.T) {
	paths := testPaths(t)
	err := Stop(paths)
	if err == nil {
		t.Fatal("expected error when no PID file")
	}
}
