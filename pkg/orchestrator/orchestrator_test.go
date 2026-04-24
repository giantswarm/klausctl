package orchestrator

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/oauth"
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

	vols, err := BuildVolumes(cfg, paths, env, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(vols) == 0 {
		t.Fatal("expected at least one volume")
	}

	found := false
	for _, v := range vols {
		if v.ContainerPath == "/workspace" { //nolint:goconst
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

func TestBuildVolumes_ContainerConfigMount(t *testing.T) {
	cfg := &config.Config{Workspace: t.TempDir()}
	paths := testPaths(t)
	env := make(map[string]string)

	vols, err := BuildVolumes(cfg, paths, env, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, v := range vols {
		if v.ContainerPath == "/etc/klaus/config.yaml" { //nolint:goconst
			found = true
			if !v.ReadOnly {
				t.Error("expected config mount to be read-only")
			}
			expectedHost := filepath.Join(paths.RenderedDir, "config.yaml")
			if v.HostPath != expectedHost {
				t.Errorf("host path = %q, want %q", v.HostPath, expectedHost)
			}
		}
	}
	if !found {
		t.Error("expected /etc/klaus/config.yaml volume mount")
	}
	if env["KLAUS_CONFIG_FILE"] != "/etc/klaus/config.yaml" {
		t.Errorf("KLAUS_CONFIG_FILE = %q, want /etc/klaus/config.yaml", env["KLAUS_CONFIG_FILE"])
	}
}

func TestBuildVolumes_McpConfigMount(t *testing.T) {
	cfg := &config.Config{
		Workspace:  t.TempDir(),
		McpServers: map[string]any{"test": map[string]any{"command": "echo"}},
	}
	paths := testPaths(t)
	env := make(map[string]string)

	vols, err := BuildVolumes(cfg, paths, env, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, v := range vols {
		if v.ContainerPath == "/etc/klaus/mcp-config.json" { //nolint:goconst
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

	vols, err := BuildVolumes(cfg, paths, env, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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
	if err := os.WriteFile(filepath.Join(personalityDir, "SOUL.md"), []byte("# Soul"), 0o600); err != nil {
		t.Fatal(err)
	}

	vols, err := BuildVolumes(cfg, paths, env, personalityDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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

	vols, err := BuildVolumes(cfg, paths, env, personalityDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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

	_, _ = BuildVolumes(cfg, paths, env, "")

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

	vols, err := BuildVolumes(cfg, paths, env, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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

func TestBuildEnvVars_SecretEnvVars(t *testing.T) {
	paths := testPaths(t)

	if err := config.EnsureDir(paths.ConfigDir); err != nil {
		t.Fatal(err)
	}

	secretsContent := "api-key: sk-secret-123\ndb-pass: hunter2\n" // #nosec G101 -- constant identifier, not a credential
	if err := os.WriteFile(paths.SecretsFile, []byte(secretsContent), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		SecretEnvVars: map[string]string{
			"ANTHROPIC_API_KEY": "api-key",
			"DB_PASSWORD":       "db-pass",
		},
	}

	env, err := BuildEnvVars(cfg, paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if env["ANTHROPIC_API_KEY"] != "sk-secret-123" {
		t.Errorf("ANTHROPIC_API_KEY = %q, want sk-secret-123", env["ANTHROPIC_API_KEY"])
	}
	if env["DB_PASSWORD"] != "hunter2" {
		t.Errorf("DB_PASSWORD = %q, want hunter2", env["DB_PASSWORD"])
	}
}

func TestBuildEnvVars_SecretEnvVars_MissingSecret(t *testing.T) {
	paths := testPaths(t)

	if err := config.EnsureDir(paths.ConfigDir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(paths.SecretsFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		SecretEnvVars: map[string]string{
			"TOKEN": "nonexistent",
		},
	}

	_, err := BuildEnvVars(cfg, paths)
	if err == nil {
		t.Error("expected error for missing secret")
	}
}

func TestBuildVolumes_SecretFiles(t *testing.T) {
	paths := testPaths(t)

	if err := config.EnsureDir(paths.ConfigDir); err != nil {
		t.Fatal(err)
	}

	secretsContent := "my-token: secret-value-123\n"
	if err := os.WriteFile(paths.SecretsFile, []byte(secretsContent), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Workspace: t.TempDir(),
		SecretFiles: map[string]string{
			"/etc/creds/token": "my-token",
		},
	}
	env := make(map[string]string)

	// Ensure the rendered dir exists for writing secret files.
	if err := config.EnsureDir(paths.RenderedDir); err != nil {
		t.Fatal(err)
	}

	vols, err := BuildVolumes(cfg, paths, env, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, v := range vols {
		if v.ContainerPath == "/etc/creds/token" {
			found = true
			if !v.ReadOnly {
				t.Error("expected secret file mount to be read-only")
			}
		}
	}
	if !found {
		t.Error("expected /etc/creds/token volume mount")
	}
}

func TestResolveSecretRefs(t *testing.T) {
	paths := testPaths(t)

	if err := config.EnsureDir(paths.ConfigDir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(paths.SecretsFile, []byte("muster-token: bearer-abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	mcpContent := "muster:\n  url: https://muster.example.com/mcp\n  secret: muster-token\n"
	if err := os.WriteFile(paths.McpServersFile, []byte(mcpContent), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Workspace:     t.TempDir(),
		McpServerRefs: []string{"muster"},
	}

	if err := ResolveSecretRefs(cfg, paths); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.McpServers == nil {
		t.Fatal("McpServers should not be nil")
	}

	entry, ok := cfg.McpServers["muster"]
	if !ok {
		t.Fatal("expected muster in McpServers")
	}

	m, ok := entry.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", entry)
	}

	if m["url"] != "https://muster.example.com/mcp" { //nolint:goconst
		t.Errorf("url = %v", m["url"])
	}
	if m["type"] != "http" {
		t.Errorf("type = %v", m["type"])
	}

	headers, ok := m["headers"].(map[string]string)
	if !ok {
		t.Fatalf("headers type = %T", m["headers"])
	}
	if headers["Authorization"] != "Bearer bearer-abc" {
		t.Errorf("Authorization = %q", headers["Authorization"])
	}
}

func TestResolveSecretRefs_NoSecret(t *testing.T) {
	paths := testPaths(t)

	if err := config.EnsureDir(paths.ConfigDir); err != nil {
		t.Fatal(err)
	}

	mcpContent := "plain:\n  url: https://plain.example.com/mcp\n"
	if err := os.WriteFile(paths.McpServersFile, []byte(mcpContent), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Workspace:     t.TempDir(),
		McpServerRefs: []string{"plain"},
	}

	if err := ResolveSecretRefs(cfg, paths); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := cfg.McpServers["plain"].(map[string]any)
	if _, hasHeaders := m["headers"]; hasHeaders {
		t.Error("expected no headers for server without secret")
	}
}

func TestResolveSecretRefs_MissingServer(t *testing.T) {
	paths := testPaths(t)

	if err := config.EnsureDir(paths.ConfigDir); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(paths.McpServersFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Workspace:     t.TempDir(),
		McpServerRefs: []string{"nonexistent"},
	}

	err := ResolveSecretRefs(cfg, paths)
	if err == nil {
		t.Error("expected error for missing MCP server")
	}
}

func TestResolveSecretRefs_Empty(t *testing.T) {
	paths := testPaths(t)
	cfg := &config.Config{Workspace: t.TempDir()}

	if err := ResolveSecretRefs(cfg, paths); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildEnvVars_GitIdentity(t *testing.T) {
	cfg := &config.Config{
		Git: config.GitConfig{
			AuthorName:  "Klaus Agent",
			AuthorEmail: "klaus@example.com",
		},
	}
	paths := testPaths(t)

	env, err := BuildEnvVars(cfg, paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if env["GIT_AUTHOR_NAME"] != "Klaus Agent" {
		t.Errorf("GIT_AUTHOR_NAME = %q, want %q", env["GIT_AUTHOR_NAME"], "Klaus Agent")
	}
	if env["GIT_COMMITTER_NAME"] != "Klaus Agent" {
		t.Errorf("GIT_COMMITTER_NAME = %q, want %q", env["GIT_COMMITTER_NAME"], "Klaus Agent")
	}
	if env["GIT_AUTHOR_EMAIL"] != "klaus@example.com" {
		t.Errorf("GIT_AUTHOR_EMAIL = %q, want %q", env["GIT_AUTHOR_EMAIL"], "klaus@example.com")
	}
	if env["GIT_COMMITTER_EMAIL"] != "klaus@example.com" {
		t.Errorf("GIT_COMMITTER_EMAIL = %q, want %q", env["GIT_COMMITTER_EMAIL"], "klaus@example.com")
	}
}

func TestBuildEnvVars_NoGitIdentityWhenEmpty(t *testing.T) {
	cfg := &config.Config{}
	paths := testPaths(t)

	env, err := BuildEnvVars(cfg, paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, key := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		if _, ok := env[key]; ok {
			t.Errorf("expected %s to be absent for empty git config", key)
		}
	}
}

func TestBuildGitConfig_Empty(t *testing.T) {
	git := &config.GitConfig{}
	if got := BuildGitConfig(git); got != "" {
		t.Errorf("expected empty gitconfig, got %q", got)
	}
}

func TestBuildGitConfig_CredentialHelper(t *testing.T) {
	git := &config.GitConfig{CredentialHelper: "gh"}
	content := BuildGitConfig(git)

	if !strings.Contains(content, `[credential "https://github.com"]`) {
		t.Error("expected github credential section")
	}
	if !strings.Contains(content, "helper = !/usr/bin/gh auth git-credential") {
		t.Error("expected gh credential helper")
	}
	if !strings.Contains(content, `[credential "https://gist.github.com"]`) {
		t.Error("expected gist credential section")
	}
}

func TestBuildGitConfig_HTTPSInsteadOfSSH(t *testing.T) {
	git := &config.GitConfig{HTTPSInsteadOfSSH: true}
	content := BuildGitConfig(git)

	if !strings.Contains(content, `[url "https://github.com/"]`) {
		t.Error("expected url rewrite section")
	}
	if !strings.Contains(content, "insteadOf = git@github.com:") {
		t.Error("expected SSH insteadOf rule")
	}
	if !strings.Contains(content, "insteadOf = ssh://git@github.com/") {
		t.Error("expected ssh:// insteadOf rule")
	}
}

func TestBuildGitConfig_Both(t *testing.T) {
	git := &config.GitConfig{
		CredentialHelper:  "gh",
		HTTPSInsteadOfSSH: true,
	}
	content := BuildGitConfig(git)

	if !strings.Contains(content, "credential") {
		t.Error("expected credential section")
	}
	if !strings.Contains(content, "insteadOf") {
		t.Error("expected insteadOf section")
	}
}

func TestBuildVolumes_GitConfigMount(t *testing.T) {
	cfg := &config.Config{
		Workspace: t.TempDir(),
		Git: config.GitConfig{
			HTTPSInsteadOfSSH: true,
		},
	}
	paths := testPaths(t)
	env := make(map[string]string)

	// Ensure rendered dir exists.
	if err := config.EnsureDir(paths.RenderedDir); err != nil {
		t.Fatal(err)
	}

	vols, err := BuildVolumes(cfg, paths, env, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, v := range vols {
		if v.ContainerPath == "/etc/klaus/gitconfig" {
			found = true
			if !v.ReadOnly {
				t.Error("expected gitconfig mount to be read-only")
			}
		}
	}
	if !found {
		t.Error("expected /etc/klaus/gitconfig volume mount")
	}
	if env["GIT_CONFIG_GLOBAL"] != "/etc/klaus/gitconfig" {
		t.Errorf("GIT_CONFIG_GLOBAL = %q, want /etc/klaus/gitconfig", env["GIT_CONFIG_GLOBAL"])
	}
}

func TestBuildVolumes_NoGitConfigWhenEmpty(t *testing.T) {
	cfg := &config.Config{Workspace: t.TempDir()}
	paths := testPaths(t)
	env := make(map[string]string)

	vols, err := BuildVolumes(cfg, paths, env, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, v := range vols {
		if v.ContainerPath == "/etc/klaus/gitconfig" {
			t.Error("expected no gitconfig mount when git config is empty")
		}
	}
	if _, ok := env["GIT_CONFIG_GLOBAL"]; ok {
		t.Error("expected GIT_CONFIG_GLOBAL to be absent")
	}
}

func TestBuildVolumes_SourcesMount(t *testing.T) {
	paths := testPaths(t)
	cfg := &config.Config{Workspace: t.TempDir()}
	env := make(map[string]string)

	// Write a sources.yaml to the config dir so it gets mounted.
	if err := config.EnsureDir(paths.ConfigDir); err != nil {
		t.Fatal(err)
	}
	sourcesContent := "sources:\n- name: giantswarm\n  registry: gsoci.azurecr.io/giantswarm\n  default: true\n- name: spiffy\n  registry: spiffy.example.com/artifacts\n"
	if err := os.WriteFile(paths.SourcesFile, []byte(sourcesContent), 0o600); err != nil {
		t.Fatal(err)
	}

	vols, err := BuildVolumes(cfg, paths, env, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, v := range vols {
		if v.ContainerPath == "/etc/klaus/sources.yaml" {
			found = true
			if !v.ReadOnly {
				t.Error("expected sources mount to be read-only")
			}
			if v.HostPath != paths.SourcesFile {
				t.Errorf("expected host path %q, got %q", paths.SourcesFile, v.HostPath)
			}
		}
	}
	if !found {
		t.Error("expected /etc/klaus/sources.yaml volume mount")
	}
	if env["KLAUSCTL_SOURCES_FILE"] != "/etc/klaus/sources.yaml" {
		t.Errorf("expected KLAUSCTL_SOURCES_FILE=/etc/klaus/sources.yaml, got %q", env["KLAUSCTL_SOURCES_FILE"])
	}
}

func TestBuildVolumes_NoSourcesMountWhenFileMissing(t *testing.T) {
	paths := testPaths(t)
	cfg := &config.Config{Workspace: t.TempDir()}
	env := make(map[string]string)

	vols, err := BuildVolumes(cfg, paths, env, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, v := range vols {
		if v.ContainerPath == "/etc/klaus/sources.yaml" {
			t.Error("expected no sources mount when sources.yaml does not exist")
		}
	}
	if _, ok := env["KLAUSCTL_SOURCES_FILE"]; ok {
		t.Error("expected KLAUSCTL_SOURCES_FILE to be absent when sources.yaml does not exist")
	}
}

func TestBuildRunOptions_ExtraHostsWithHostDockerInternal(t *testing.T) {
	workspace := t.TempDir()
	cfg := &config.Config{
		Workspace: workspace,
		Port:      9090,
		McpServers: map[string]any{
			"muster": map[string]any{
				"url":  "http://host.docker.internal:8090/mcp",
				"type": "http",
			},
		},
	}
	paths := testPaths(t)

	opts, err := BuildRunOptions(cfg, paths, "test-container", "test-image:latest", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// On Linux, host.docker.internal mapping should be added automatically.
	if goruntime.GOOS == "linux" {
		expected := "host.docker.internal:host-gateway"
		found := false
		for _, h := range opts.ExtraHosts {
			if h == expected {
				found = true
			}
		}
		if !found {
			t.Errorf("expected ExtraHosts to contain %q on Linux, got %v", expected, opts.ExtraHosts)
		}
	} else {
		if len(opts.ExtraHosts) > 0 {
			t.Errorf("expected empty ExtraHosts on non-Linux, got %v", opts.ExtraHosts)
		}
	}
}

func TestBuildRunOptions_NoExtraHostsWithoutHostDockerInternal(t *testing.T) {
	workspace := t.TempDir()
	cfg := &config.Config{
		Workspace: workspace,
		Port:      9090,
		McpServers: map[string]any{
			"remote": map[string]any{
				"url":  "https://mcp.example.com/mcp",
				"type": "http",
			},
		},
	}
	paths := testPaths(t)

	opts, err := BuildRunOptions(cfg, paths, "test-container", "test-image:latest", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(opts.ExtraHosts) > 0 {
		t.Errorf("expected empty ExtraHosts when no host.docker.internal URL, got %v", opts.ExtraHosts)
	}
}

func TestBuildRunOptions_NoExtraHostsWithoutMcpServers(t *testing.T) {
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

	if len(opts.ExtraHosts) > 0 {
		t.Errorf("expected empty ExtraHosts with no MCP servers, got %v", opts.ExtraHosts)
	}
}

func TestResolveSecretRefs_OAuthToken(t *testing.T) {
	paths := testPaths(t)

	if err := config.EnsureDir(paths.ConfigDir); err != nil {
		t.Fatal(err)
	}

	serverURL := "https://muster.example.com/mcp"
	mcpContent := "muster-oauth:\n  url: " + serverURL + "\n"
	if err := os.WriteFile(paths.McpServersFile, []byte(mcpContent), 0o600); err != nil {
		t.Fatal(err)
	}

	tokenStore := oauth.NewTokenStore(paths.TokensDir)
	if err := tokenStore.StoreToken(serverURL, "https://dex.example.com", oauth.Token{ // #nosec G101 -- constant identifier, not a credential
		AccessToken: "oauth-access-token-xyz",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
	}); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Workspace:     t.TempDir(),
		McpServerRefs: []string{"muster-oauth"},
	}

	if err := ResolveSecretRefs(cfg, paths); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry, ok := cfg.McpServers["muster-oauth"]
	if !ok {
		t.Fatal("expected muster-oauth in McpServers")
	}

	m := entry.(map[string]any)
	headers, ok := m["headers"].(map[string]string)
	if !ok {
		t.Fatalf("expected headers map, got %T", m["headers"])
	}
	if headers["Authorization"] != "Bearer oauth-access-token-xyz" {
		t.Errorf("Authorization = %q, want %q", headers["Authorization"], "Bearer oauth-access-token-xyz")
	}
}

func TestResolveSecretRefs_OAuthTokenExpired(t *testing.T) {
	paths := testPaths(t)

	if err := config.EnsureDir(paths.ConfigDir); err != nil {
		t.Fatal(err)
	}

	serverURL := "https://muster.example.com/mcp"
	mcpContent := "muster-expired:\n  url: " + serverURL + "\n"
	if err := os.WriteFile(paths.McpServersFile, []byte(mcpContent), 0o600); err != nil {
		t.Fatal(err)
	}

	tokenStore := oauth.NewTokenStore(paths.TokensDir)
	if err := tokenStore.StoreToken(serverURL, "https://dex.example.com", oauth.Token{
		AccessToken: "expired-token",
		TokenType:   "Bearer",
		ExpiresIn:   1,
	}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(2 * time.Second)

	cfg := &config.Config{
		Workspace:     t.TempDir(),
		McpServerRefs: []string{"muster-expired"},
	}

	if err := ResolveSecretRefs(cfg, paths); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := cfg.McpServers["muster-expired"].(map[string]any)
	if _, hasHeaders := m["headers"]; hasHeaders {
		t.Error("expected no headers for expired OAuth token")
	}
}

func TestResolveSecretRefs_StaticSecretTakesPrecedence(t *testing.T) {
	paths := testPaths(t)

	if err := config.EnsureDir(paths.ConfigDir); err != nil {
		t.Fatal(err)
	}

	serverURL := "https://muster.example.com/mcp"

	if err := os.WriteFile(paths.SecretsFile, []byte("static-secret: static-token-123\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	mcpContent := "muster-both:\n  url: " + serverURL + "\n  secret: static-secret\n"
	if err := os.WriteFile(paths.McpServersFile, []byte(mcpContent), 0o600); err != nil {
		t.Fatal(err)
	}

	tokenStore := oauth.NewTokenStore(paths.TokensDir)
	if err := tokenStore.StoreToken(serverURL, "https://dex.example.com", oauth.Token{ // #nosec G101 -- constant identifier, not a credential
		AccessToken: "oauth-token-should-not-be-used",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
	}); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Workspace:     t.TempDir(),
		McpServerRefs: []string{"muster-both"},
	}

	if err := ResolveSecretRefs(cfg, paths); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := cfg.McpServers["muster-both"].(map[string]any)
	headers := m["headers"].(map[string]string)
	if headers["Authorization"] != "Bearer static-token-123" {
		t.Errorf("Authorization = %q, want static secret not OAuth token", headers["Authorization"])
	}
}

// Verify RunOptions types match expected runtime types (compilation check).
var _ runtime.RunOptions = runtime.RunOptions{}
