package orchestrator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

func TestBuildEnvVars_Defaults(t *testing.T) {
	cfg := &config.Config{Port: 8080}
	paths := testPaths(t)

	env, err := BuildEnvVars(cfg, paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if env["PORT"] != "8080" {
		t.Errorf("expected PORT=8080, got %q", env["PORT"])
	}
}

func TestBuildEnvVars_EnvForward(t *testing.T) {
	t.Setenv("MY_CUSTOM_VAR", "hello")
	cfg := &config.Config{EnvForward: []string{"MY_CUSTOM_VAR", "UNSET_VAR"}}
	paths := testPaths(t)

	env, err := BuildEnvVars(cfg, paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if env["MY_CUSTOM_VAR"] != "hello" {
		t.Errorf("expected MY_CUSTOM_VAR=hello, got %q", env["MY_CUSTOM_VAR"])
	}
	if _, ok := env["UNSET_VAR"]; ok {
		t.Error("expected UNSET_VAR to be absent")
	}
}

func TestBuildEnvVars_ExplicitEnvVars(t *testing.T) {
	cfg := &config.Config{
		EnvVars: map[string]string{"FOO": "bar", "BAZ": "qux"},
	}
	paths := testPaths(t)

	env, err := BuildEnvVars(cfg, paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if env["FOO"] != "bar" {
		t.Errorf("expected FOO=bar, got %q", env["FOO"])
	}
	if env["BAZ"] != "qux" {
		t.Errorf("expected BAZ=qux, got %q", env["BAZ"])
	}
}

func TestBuildEnvVars_ClaudeModel(t *testing.T) {
	cfg := &config.Config{
		Claude: config.ClaudeConfig{
			Model:          "sonnet",
			PermissionMode: "bypassPermissions",
			MaxTurns:       10,
			MaxBudgetUSD:   5.50,
		},
	}
	paths := testPaths(t)

	env, err := BuildEnvVars(cfg, paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if env["CLAUDE_MODEL"] != "sonnet" {
		t.Errorf("expected CLAUDE_MODEL=sonnet, got %q", env["CLAUDE_MODEL"])
	}
	if env["CLAUDE_PERMISSION_MODE"] != "bypassPermissions" {
		t.Errorf("expected CLAUDE_PERMISSION_MODE=bypassPermissions, got %q", env["CLAUDE_PERMISSION_MODE"])
	}
	if env["CLAUDE_MAX_TURNS"] != "10" {
		t.Errorf("expected CLAUDE_MAX_TURNS=10, got %q", env["CLAUDE_MAX_TURNS"])
	}
	if env["CLAUDE_MAX_BUDGET_USD"] != "5.50" {
		t.Errorf("expected CLAUDE_MAX_BUDGET_USD=5.50, got %q", env["CLAUDE_MAX_BUDGET_USD"])
	}
}

func TestBuildEnvVars_ClaudeTools(t *testing.T) {
	cfg := &config.Config{
		Claude: config.ClaudeConfig{
			Tools:           []string{"read", "write"},
			AllowedTools:    []string{"mcp__*"},
			DisallowedTools: []string{"bash"},
		},
	}
	paths := testPaths(t)

	env, err := BuildEnvVars(cfg, paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if env["CLAUDE_TOOLS"] != "read,write" {
		t.Errorf("expected CLAUDE_TOOLS=read,write, got %q", env["CLAUDE_TOOLS"])
	}
	if env["CLAUDE_ALLOWED_TOOLS"] != "mcp__*" {
		t.Errorf("expected CLAUDE_ALLOWED_TOOLS=mcp__*, got %q", env["CLAUDE_ALLOWED_TOOLS"])
	}
	if env["CLAUDE_DISALLOWED_TOOLS"] != "bash" {
		t.Errorf("expected CLAUDE_DISALLOWED_TOOLS=bash, got %q", env["CLAUDE_DISALLOWED_TOOLS"])
	}
}

func TestBuildEnvVars_EmptyClaudeFieldsOmitted(t *testing.T) {
	cfg := &config.Config{
		Claude: config.ClaudeConfig{},
	}
	paths := testPaths(t)

	env, err := BuildEnvVars(cfg, paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, key := range []string{
		"CLAUDE_MODEL", "CLAUDE_SYSTEM_PROMPT", "CLAUDE_MAX_TURNS",
		"CLAUDE_TOOLS", "CLAUDE_ALLOWED_TOOLS", "CLAUDE_DISALLOWED_TOOLS",
	} {
		if _, ok := env[key]; ok {
			t.Errorf("expected %s to be absent for zero-value config", key)
		}
	}
}

func TestBuildVolumes_WorkspaceMount(t *testing.T) {
	workspace := t.TempDir()
	cfg := &config.Config{Workspace: workspace}
	paths := testPaths(t)
	env := make(map[string]string)

	vols := BuildVolumes(cfg, paths, env, "")

	if len(vols) == 0 {
		t.Fatal("expected at least one volume")
	}

	found := false
	for _, v := range vols {
		if v.ContainerPath == "/workspace" {
			found = true
			if v.HostPath != workspace {
				t.Errorf("expected workspace host path %q, got %q", workspace, v.HostPath)
			}
		}
	}
	if !found {
		t.Error("expected /workspace volume mount")
	}
	if env["CLAUDE_WORKSPACE"] != "/workspace" {
		t.Errorf("expected CLAUDE_WORKSPACE=/workspace, got %q", env["CLAUDE_WORKSPACE"])
	}
}

func TestBuildVolumes_McpConfigMount(t *testing.T) {
	cfg := &config.Config{
		Workspace:  t.TempDir(),
		McpServers: map[string]any{"test": map[string]any{"command": "echo"}},
	}
	paths := testPaths(t)
	env := make(map[string]string)

	vols := BuildVolumes(cfg, paths, env, "")

	found := false
	for _, v := range vols {
		if v.ContainerPath == "/etc/klaus/mcp-config.json" {
			found = true
			if !v.ReadOnly {
				t.Error("expected mcp-config mount to be read-only")
			}
		}
	}
	if !found {
		t.Error("expected /etc/klaus/mcp-config.json volume mount")
	}
	if env["CLAUDE_MCP_CONFIG"] != "/etc/klaus/mcp-config.json" {
		t.Errorf("expected CLAUDE_MCP_CONFIG set, got %q", env["CLAUDE_MCP_CONFIG"])
	}
}

func TestBuildVolumes_NoMcpConfigWhenEmpty(t *testing.T) {
	cfg := &config.Config{Workspace: t.TempDir()}
	paths := testPaths(t)
	env := make(map[string]string)

	vols := BuildVolumes(cfg, paths, env, "")

	for _, v := range vols {
		if v.ContainerPath == "/etc/klaus/mcp-config.json" {
			t.Error("expected no mcp-config mount when McpServers is empty")
		}
	}
	if _, ok := env["CLAUDE_MCP_CONFIG"]; ok {
		t.Error("expected CLAUDE_MCP_CONFIG to be absent")
	}
}

func TestBuildRunOptions_Structure(t *testing.T) {
	workspace := t.TempDir()
	cfg := &config.Config{
		Workspace: workspace,
		Port:      9090,
	}
	paths := testPaths(t)

	opts, err := BuildRunOptions(cfg, paths, "test-container", "test-image:latest", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if opts.Name != "test-container" {
		t.Errorf("expected name test-container, got %q", opts.Name)
	}
	if opts.Image != "test-image:latest" {
		t.Errorf("expected image test-image:latest, got %q", opts.Image)
	}
	if !opts.Detach {
		t.Error("expected Detach=true")
	}
	if opts.Ports[9090] != 8080 {
		t.Errorf("expected port mapping 9090:8080, got %v", opts.Ports)
	}

	hasWorkspaceVol := false
	for _, v := range opts.Volumes {
		if v.ContainerPath == "/workspace" {
			hasWorkspaceVol = true
		}
	}
	if !hasWorkspaceVol {
		t.Error("expected /workspace volume in RunOptions")
	}
}

func TestBuildVolumes_PersonalitySOULMount(t *testing.T) {
	cfg := &config.Config{Workspace: t.TempDir()}
	paths := testPaths(t)
	env := make(map[string]string)

	personalityDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(personalityDir, "SOUL.md"), []byte("# Soul"), 0o644); err != nil {
		t.Fatal(err)
	}

	vols := BuildVolumes(cfg, paths, env, personalityDir)

	found := false
	for _, v := range vols {
		if v.ContainerPath == "/etc/klaus/SOUL.md" {
			found = true
			if !v.ReadOnly {
				t.Error("expected SOUL.md mount to be read-only")
			}
		}
	}
	if !found {
		t.Error("expected /etc/klaus/SOUL.md volume mount")
	}
}

func TestBuildVolumes_NoSOULWithoutFile(t *testing.T) {
	cfg := &config.Config{Workspace: t.TempDir()}
	paths := testPaths(t)
	env := make(map[string]string)

	personalityDir := t.TempDir()

	vols := BuildVolumes(cfg, paths, env, personalityDir)

	for _, v := range vols {
		if v.ContainerPath == "/etc/klaus/SOUL.md" {
			t.Error("expected no SOUL.md mount when file is absent")
		}
	}
}

func TestBuildVolumes_SettingsFileFromClaudeConfig(t *testing.T) {
	cfg := &config.Config{
		Workspace: t.TempDir(),
		Claude:    config.ClaudeConfig{SettingsFile: "/custom/settings.json"},
	}
	paths := testPaths(t)
	env := make(map[string]string)

	_ = BuildVolumes(cfg, paths, env, "")

	if env["CLAUDE_SETTINGS_FILE"] != "/custom/settings.json" {
		t.Errorf("expected CLAUDE_SETTINGS_FILE=/custom/settings.json, got %q", env["CLAUDE_SETTINGS_FILE"])
	}
}

func TestBuildVolumes_Plugins(t *testing.T) {
	cfg := &config.Config{
		Workspace: t.TempDir(),
		Plugins: []config.Plugin{
			{Repository: "gsoci.azurecr.io/giantswarm/klaus-plugin-test"},
		},
	}
	paths := testPaths(t)
	env := make(map[string]string)

	vols := BuildVolumes(cfg, paths, env, "")

	expectedMount := "/var/lib/klaus/plugins/klaus-plugin-test"
	found := false
	for _, v := range vols {
		if v.ContainerPath == expectedMount {
			found = true
			if !v.ReadOnly {
				t.Error("expected plugin mount to be read-only")
			}
		}
	}
	if !found {
		var containerPaths []string
		for _, v := range vols {
			containerPaths = append(containerPaths, v.ContainerPath)
		}
		t.Errorf("expected plugin volume mount at %s, got %v", expectedMount, containerPaths)
	}
}

// testPaths returns config paths rooted in a temp directory.
func testPaths(t *testing.T) *config.Paths {
	t.Helper()
	configHome := filepath.Join(t.TempDir(), "config-home")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

// Verify RunOptions types match expected runtime types (compilation check).
var _ runtime.RunOptions = runtime.RunOptions{}
