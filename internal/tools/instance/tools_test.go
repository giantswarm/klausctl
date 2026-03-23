package instance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"gopkg.in/yaml.v3"

	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/mcpclient"
)

func testServerContext(t *testing.T) *server.ServerContext {
	t.Helper()
	configHome := filepath.Join(t.TempDir(), "config-home")
	t.Setenv("XDG_CONFIG_HOME", configHome)

	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.EnsureDir(paths.InstancesDir); err != nil {
		t.Fatal(err)
	}
	return &server.ServerContext{Paths: paths, MCPClient: mcpclient.New("test")}
}

func TestRegisterTools(t *testing.T) {
	sc := testServerContext(t)
	srv := mcpserver.NewMCPServer("test", "1.0.0",
		mcpserver.WithToolCapabilities(false),
	)
	RegisterTools(srv, sc)
}

func TestHandleListEmpty(t *testing.T) {
	sc := testServerContext(t)

	req := mcp.CallToolRequest{}
	result, err := handleList(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertJSONArray(t, result, 0)
}

func TestHandleStatusMissingInstance(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{"name": "nonexistent"})
	result, err := handleStatus(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertIsError(t, result)
}

func TestHandleStatusStoppedInstance(t *testing.T) {
	sc := testServerContext(t)

	instanceDir := filepath.Join(sc.Paths.InstancesDir, "stopped-inst")
	if err := os.MkdirAll(instanceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.Image = "example.com/test:v1"
	cfg.Workspace = "/tmp"
	data, err := cfg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instanceDir, "config.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	req := callToolRequest(map[string]any{"name": "stopped-inst"})
	result, err := handleStatus(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractResultText(t, result)
	var obj map[string]string
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		t.Fatalf("expected JSON object, got: %s", text)
	}
	if obj["status"] != "stopped" {
		t.Errorf("expected 'stopped' status, got %q", obj["status"])
	}
	if obj["instance"] != "stopped-inst" {
		t.Errorf("expected instance 'stopped-inst', got %q", obj["instance"])
	}
}

func TestHandleLogsMissingInstance(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{"name": "nonexistent"})
	result, err := handleLogs(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertIsError(t, result)
}

func TestHandleDeleteMissingInstance(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{"name": "nonexistent"})
	result, err := handleDelete(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertIsError(t, result)
}

func TestHandleCreatePortConflict(t *testing.T) {
	sc := testServerContext(t)

	workspace := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	// Seed an existing instance with port 9090.
	conflictDir := filepath.Join(sc.Paths.InstancesDir, "other")
	if err := os.MkdirAll(conflictDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conflictDir, "config.yaml"), []byte("workspace: /tmp\nport: 9090\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := callToolRequest(map[string]any{
		"name":      "porttest",
		"workspace": workspace,
		"port":      float64(9090),
	})
	result, err := handleCreate(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertIsError(t, result)
	text := extractResultText(t, result)
	if !strings.Contains(text, "already used") {
		t.Fatalf("expected port conflict error, got: %s", text)
	}
}

func TestHandleCreateCustomPort(t *testing.T) {
	sc := testServerContext(t)

	workspace := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	req := callToolRequest(map[string]any{
		"name":      "portcustom",
		"workspace": workspace,
		"port":      float64(9999),
	})
	result, err := handleCreate(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The create will fail because there's no container runtime in test, but
	// the config file should be written before that stage. Verify port was
	// correctly wired through.
	configPath := filepath.Join(sc.Paths.InstancesDir, "portcustom", "config.yaml")
	data, readErr := os.ReadFile(configPath)
	if readErr != nil {
		// Config was not written; verify at minimum that the error is not a port error.
		if result.IsError {
			text := extractResultText(t, result)
			if strings.Contains(text, "already used") || strings.Contains(text, "port must be") {
				t.Fatalf("unexpected port error: %s", text)
			}
		}
		return
	}

	var cfgMap map[string]any
	if err := yaml.Unmarshal(data, &cfgMap); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}
	if port, ok := cfgMap["port"]; !ok {
		t.Fatal("port not found in config")
	} else if portInt, ok := port.(int); !ok || portInt != 9999 {
		t.Fatalf("expected port 9999 in config, got %v", port)
	}
}

func TestHandleCreatePortOutOfRange(t *testing.T) {
	sc := testServerContext(t)

	workspace := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		port float64
	}{
		{"negative", -1},
		{"too large", 70000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := callToolRequest(map[string]any{
				"name":      "rangetest",
				"workspace": workspace,
				"port":      tt.port,
			})
			result, err := handleCreate(context.Background(), req, sc)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertIsError(t, result)
			text := extractResultText(t, result)
			if !strings.Contains(text, "port must be") {
				t.Fatalf("expected port range error, got: %s", text)
			}
		})
	}
}

func TestHandleCreateInvalidName(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{"name": "INVALID NAME!"})
	result, err := handleCreate(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertIsError(t, result)
}

func TestHandleCreateDuplicateInstance(t *testing.T) {
	sc := testServerContext(t)

	workspace := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	instanceDir := filepath.Join(sc.Paths.InstancesDir, "existing")
	if err := os.MkdirAll(instanceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instanceDir, "config.yaml"), []byte("workspace: /tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := callToolRequest(map[string]any{
		"name":           "existing",
		"workspace":      workspace,
		"generateSuffix": false,
	})
	result, err := handleCreate(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertIsError(t, result)
	text := extractResultText(t, result)
	if !strings.Contains(text, "already exists") {
		t.Fatalf("expected collision error, got: %s", text)
	}
}

func TestHandleCreateMCPCollisionStoppedWithoutConfirm(t *testing.T) {
	sc := testServerContext(t)

	workspace := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	instanceDir := filepath.Join(sc.Paths.InstancesDir, "stopped")
	if err := os.MkdirAll(instanceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instanceDir, "config.yaml"), []byte("workspace: /tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := callToolRequest(map[string]any{
		"name":           "stopped",
		"workspace":      workspace,
		"generateSuffix": false,
	})
	result, err := handleCreate(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertIsError(t, result)
	text := extractResultText(t, result)
	if !strings.Contains(text, "confirm: true") {
		t.Fatalf("expected error mentioning confirm: true, got: %s", text)
	}
}

func TestHandleCreateMCPCollisionStoppedWithConfirm(t *testing.T) {
	sc := testServerContext(t)

	workspace := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	instanceDir := filepath.Join(sc.Paths.InstancesDir, "stopped")
	if err := os.MkdirAll(instanceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	markerFile := filepath.Join(instanceDir, "old-marker.txt")
	if err := os.WriteFile(markerFile, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instanceDir, "config.yaml"), []byte("workspace: /tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := callToolRequest(map[string]any{
		"name":           "stopped",
		"workspace":      workspace,
		"generateSuffix": false,
		"confirm":        true,
	})
	result, err := handleCreate(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The old directory should have been cleaned up. Create will fail at
	// startExistingInstance (no runtime), but old state should be gone.
	if _, err := os.Stat(markerFile); !os.IsNotExist(err) {
		t.Fatal("expected old marker file to be removed by collision cleanup")
	}

	// Should still get an error (from no runtime), but not a collision error.
	assertIsError(t, result)
	text := extractResultText(t, result)
	if strings.Contains(text, "already exists") {
		t.Fatalf("should not get collision error after confirm, got: %s", text)
	}
}

func TestHandleCreateMCPCollisionSuffixAvoidsCollision(t *testing.T) {
	sc := testServerContext(t)

	workspace := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create "myinst" directory — with suffix generation enabled, the
	// generated name "myinst-XXXX" won't collide.
	instanceDir := filepath.Join(sc.Paths.InstancesDir, "myinst")
	if err := os.MkdirAll(instanceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instanceDir, "config.yaml"), []byte("workspace: /tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := callToolRequest(map[string]any{
		"name":           "myinst",
		"workspace":      workspace,
		"generateSuffix": true,
	})
	result, err := handleCreate(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should fail at runtime (no Docker), not at collision.
	assertIsError(t, result)
	text := extractResultText(t, result)
	if strings.Contains(text, "already exists") || strings.Contains(text, "confirm") {
		t.Fatalf("expected runtime error not collision error, got: %s", text)
	}
}

func TestHandleCreateGitAuthor(t *testing.T) {
	sc := testServerContext(t)

	workspace := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	req := callToolRequest(map[string]any{
		"name":      "gitauthor",
		"workspace": workspace,
		"gitAuthor": "Test User <test@example.com>",
	})
	result, err := handleCreate(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	configPath := filepath.Join(sc.Paths.InstancesDir, "gitauthor", "config.yaml")
	data, readErr := os.ReadFile(configPath)
	if readErr != nil {
		if result.IsError {
			text := extractResultText(t, result)
			if strings.Contains(text, "gitAuthor") {
				t.Fatalf("unexpected git author error: %s", text)
			}
		}
		return
	}

	var cfgMap map[string]any
	if err := yaml.Unmarshal(data, &cfgMap); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}
	git, ok := cfgMap["git"].(map[string]any)
	if !ok {
		t.Fatal("git section not found in config")
	}
	if git["authorName"] != "Test User" {
		t.Errorf("expected authorName 'Test User', got %v", git["authorName"])
	}
	if git["authorEmail"] != "test@example.com" {
		t.Errorf("expected authorEmail 'test@example.com', got %v", git["authorEmail"])
	}
}

func TestHandleCreateGitAuthorInvalidFormat(t *testing.T) {
	sc := testServerContext(t)

	workspace := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	req := callToolRequest(map[string]any{
		"name":      "gitbad",
		"workspace": workspace,
		"gitAuthor": "no angle brackets",
	})
	result, err := handleCreate(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertIsError(t, result)
	text := extractResultText(t, result)
	if !strings.Contains(text, "gitAuthor") {
		t.Fatalf("expected gitAuthor format error, got: %s", text)
	}
}

func TestHandleCreateGitCredentialHelper(t *testing.T) {
	sc := testServerContext(t)

	workspace := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	req := callToolRequest(map[string]any{
		"name":                "gitcred",
		"workspace":           workspace,
		"gitCredentialHelper": "gh",
	})
	result, err := handleCreate(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	configPath := filepath.Join(sc.Paths.InstancesDir, "gitcred", "config.yaml")
	data, readErr := os.ReadFile(configPath)
	if readErr != nil {
		if result.IsError {
			text := extractResultText(t, result)
			if strings.Contains(text, "credential") {
				t.Fatalf("unexpected credential helper error: %s", text)
			}
		}
		return
	}

	var cfgMap map[string]any
	if err := yaml.Unmarshal(data, &cfgMap); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}
	git, ok := cfgMap["git"].(map[string]any)
	if !ok {
		t.Fatal("git section not found in config")
	}
	if git["credentialHelper"] != "gh" {
		t.Errorf("expected credentialHelper 'gh', got %v", git["credentialHelper"])
	}
}

func TestHandleCreateGitHttpsInsteadOfSsh(t *testing.T) {
	sc := testServerContext(t)

	workspace := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	req := callToolRequest(map[string]any{
		"name":                 "githttps",
		"workspace":            workspace,
		"gitHttpsInsteadOfSsh": true,
	})
	result, err := handleCreate(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	configPath := filepath.Join(sc.Paths.InstancesDir, "githttps", "config.yaml")
	data, readErr := os.ReadFile(configPath)
	if readErr != nil {
		if result.IsError {
			text := extractResultText(t, result)
			if strings.Contains(text, "https") || strings.Contains(text, "SSH") {
				t.Fatalf("unexpected git HTTPS error: %s", text)
			}
		}
		return
	}

	var cfgMap map[string]any
	if err := yaml.Unmarshal(data, &cfgMap); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}
	git, ok := cfgMap["git"].(map[string]any)
	if !ok {
		t.Fatal("git section not found in config")
	}
	if git["httpsInsteadOfSsh"] != true {
		t.Errorf("expected httpsInsteadOfSsh true, got %v", git["httpsInsteadOfSsh"])
	}
}

func TestHandleCreateModeChat(t *testing.T) {
	sc := testServerContext(t)

	workspace := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	req := callToolRequest(map[string]any{
		"name":      "chatmode",
		"workspace": workspace,
		"mode":      "chat",
	})
	result, err := handleCreate(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	configPath := filepath.Join(sc.Paths.InstancesDir, "chatmode", "config.yaml")
	data, readErr := os.ReadFile(configPath)
	if readErr != nil {
		if result.IsError {
			text := extractResultText(t, result)
			if strings.Contains(text, "mode") {
				t.Fatalf("unexpected mode error: %s", text)
			}
		}
		return
	}

	var cfgMap map[string]any
	if err := yaml.Unmarshal(data, &cfgMap); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}
	claude, ok := cfgMap["claude"].(map[string]any)
	if !ok {
		t.Fatal("claude section not found in config")
	}
	if claude["mode"] != "chat" {
		t.Errorf("expected mode=chat, got %v", claude["mode"])
	}
}

func TestHandleCreateModeDefaultAgent(t *testing.T) {
	sc := testServerContext(t)

	workspace := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	req := callToolRequest(map[string]any{
		"name":      "agentmode",
		"workspace": workspace,
	})
	result, err := handleCreate(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	configPath := filepath.Join(sc.Paths.InstancesDir, "agentmode", "config.yaml")
	data, readErr := os.ReadFile(configPath)
	if readErr != nil {
		if result.IsError {
			text := extractResultText(t, result)
			if strings.Contains(text, "mode") {
				t.Fatalf("unexpected mode error: %s", text)
			}
		}
		return
	}

	var cfgMap map[string]any
	if err := yaml.Unmarshal(data, &cfgMap); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}
	claude, ok := cfgMap["claude"].(map[string]any)
	if !ok {
		t.Fatal("claude section not found in config")
	}
	if claude["mode"] != "agent" {
		t.Errorf("expected mode=agent by default, got %v", claude["mode"])
	}
}

func TestHandleCreateAllGitParams(t *testing.T) {
	sc := testServerContext(t)

	workspace := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	req := callToolRequest(map[string]any{
		"name":                 "gitall",
		"workspace":            workspace,
		"gitAuthor":            "Dev User <dev@example.com>",
		"gitCredentialHelper":  "gh",
		"gitHttpsInsteadOfSsh": true,
	})
	result, err := handleCreate(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	configPath := filepath.Join(sc.Paths.InstancesDir, "gitall", "config.yaml")
	data, readErr := os.ReadFile(configPath)
	if readErr != nil {
		if result.IsError {
			text := extractResultText(t, result)
			if strings.Contains(text, "gitAuthor") || strings.Contains(text, "credential") {
				t.Fatalf("unexpected git config error: %s", text)
			}
		}
		return
	}

	var cfgMap map[string]any
	if err := yaml.Unmarshal(data, &cfgMap); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}
	git, ok := cfgMap["git"].(map[string]any)
	if !ok {
		t.Fatal("git section not found in config")
	}
	if git["authorName"] != "Dev User" {
		t.Errorf("expected authorName 'Dev User', got %v", git["authorName"])
	}
	if git["authorEmail"] != "dev@example.com" {
		t.Errorf("expected authorEmail 'dev@example.com', got %v", git["authorEmail"])
	}
	if git["credentialHelper"] != "gh" {
		t.Errorf("expected credentialHelper 'gh', got %v", git["credentialHelper"])
	}
	if git["httpsInsteadOfSsh"] != true {
		t.Errorf("expected httpsInsteadOfSsh true, got %v", git["httpsInsteadOfSsh"])
	}
}

func TestParseGitAuthor(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantName  string
		wantEmail string
		wantErr   bool
	}{
		{"empty", "", "", "", false},
		{"valid", "Jane Doe <jane@example.com>", "Jane Doe", "jane@example.com", false},
		{"no angle brackets", "just a name", "", "", true},
		{"empty name", " <email@example.com>", "", "", true},
		{"empty email", "Name <>", "", "", true},
		{"reversed brackets", "Name >email<", "", "", true},
		{"trailing content", "Name <a@b.com> extra", "", "", true},
		{"newline in name", "Evil\nName <a@b.com>", "", "", true},
		{"newline in email", "Name <a@b.com\nfoo>", "", "", true},
		{"null byte in name", "Name\x00 <a@b.com>", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, email, err := parseGitAuthor(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseGitAuthor(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if name != tt.wantName {
				t.Errorf("parseGitAuthor(%q) name = %q, want %q", tt.input, name, tt.wantName)
			}
			if email != tt.wantEmail {
				t.Errorf("parseGitAuthor(%q) email = %q, want %q", tt.input, email, tt.wantEmail)
			}
		})
	}
}

func TestHandleStopRequiresNameOrAll(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{})
	result, err := handleStop(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertIsError(t, result)
}

func TestHandleStopNameAndAllMutuallyExclusive(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{
		"name": "test",
		"all":  true,
	})
	result, err := handleStop(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertIsError(t, result)
}

func TestHandleStopNotRunning(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{"name": "nonexistent"})
	result, err := handleStop(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := extractResultText(t, result)
	var obj map[string]string
	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		t.Fatalf("expected JSON object, got: %s", data)
	}
	if obj["status"] != "not running" {
		t.Errorf("expected 'not running' status, got %q", obj["status"])
	}
}

func TestHandleStopAllEmpty(t *testing.T) {
	sc := testServerContext(t)

	req := callToolRequest(map[string]any{"all": true})
	result, err := handleStop(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := extractResultText(t, result)
	var obj map[string]any
	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		t.Fatalf("expected JSON object, got: %s", data)
	}
	if obj["status"] != "all stopped" {
		t.Errorf("expected 'all stopped', got %v", obj["status"])
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"seconds", 30 * time.Second, "30s"},
		{"minutes", 150 * time.Second, "2m30s"},
		{"hours", 90 * time.Minute, "1h30m"},
		{"days", 25 * time.Hour, "1d1h"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.d)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

// --- helpers ---

func callToolRequest(args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: args,
		},
	}
}

func assertIsError(t *testing.T, result *mcp.CallToolResult) {
	t.Helper()
	if result == nil {
		t.Fatal("result is nil")
	}
	if !result.IsError {
		t.Errorf("expected error result, got success: %+v", result)
	}
}

func assertJSONArray(t *testing.T, result *mcp.CallToolResult, expectedLen int) {
	t.Helper()
	data := extractResultText(t, result)
	var arr []any
	if err := json.Unmarshal([]byte(data), &arr); err != nil {
		t.Fatalf("expected JSON array, got: %s", data)
	}
	if len(arr) != expectedLen {
		t.Errorf("expected %d elements, got %d", expectedLen, len(arr))
	}
}

func extractResultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result == nil {
		t.Fatal("result is nil")
	}
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	content := result.Content[0]
	textContent, ok := content.(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", content)
	}
	return textContent.Text
}
