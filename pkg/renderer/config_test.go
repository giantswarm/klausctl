package renderer

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/giantswarm/klausctl/pkg/config"
)

func TestBuildContainerConfig_FixedContainerValues(t *testing.T) {
	cfg := &config.Config{
		Workspace: "/home/user/project",
		Port:      9090,
	}

	cc := BuildContainerConfig(cfg)

	// Workspace and Port are always the container-internal values,
	// regardless of what the host config specifies.
	if cc.Workspace != "/workspace" {
		t.Errorf("Workspace = %q, want /workspace", cc.Workspace)
	}
	if cc.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cc.Port)
	}
}

func TestBuildContainerConfig_ClaudeSettings(t *testing.T) {
	cfg := &config.Config{
		Workspace: "/tmp/ws",
		Port:      8080,
		Claude: config.ClaudeConfig{
			Model:                "opus",
			SystemPrompt:         "You are helpful.",
			AppendSystemPrompt:   "Be concise.",
			PermissionMode:       "bypassPermissions",
			Effort:               "high",
			FallbackModel:        "sonnet",
			MaxTurns:             50,
			MaxBudgetUSD:         10.0,
			StrictMcpConfig:      true,
			McpTimeout:           30000,
			MaxMcpOutputTokens:   8192,
			ActiveAgent:          "reviewer",
			Mode:                 "chat",
			Tools:                []string{"read", "write"},
			AllowedTools:         []string{"mcp__*"},
			DisallowedTools:      []string{"bash"},
		},
	}

	cc := BuildContainerConfig(cfg)

	if cc.Claude.Model != "opus" {
		t.Errorf("Claude.Model = %q, want opus", cc.Claude.Model)
	}
	if cc.Claude.SystemPrompt != "You are helpful." {
		t.Errorf("Claude.SystemPrompt = %q", cc.Claude.SystemPrompt)
	}
	if cc.Claude.PermissionMode != "bypassPermissions" {
		t.Errorf("Claude.PermissionMode = %q", cc.Claude.PermissionMode)
	}
	if cc.Claude.MaxTurns != 50 {
		t.Errorf("Claude.MaxTurns = %d, want 50", cc.Claude.MaxTurns)
	}
	if cc.Claude.MaxBudgetUSD != 10.0 {
		t.Errorf("Claude.MaxBudgetUSD = %f, want 10.0", cc.Claude.MaxBudgetUSD)
	}
	if !cc.Claude.StrictMcpConfig {
		t.Error("Claude.StrictMcpConfig should be true")
	}
	if len(cc.Claude.Tools) != 2 {
		t.Errorf("Claude.Tools length = %d, want 2", len(cc.Claude.Tools))
	}
	if cc.Claude.Mode != "chat" {
		t.Errorf("Claude.Mode = %q, want chat", cc.Claude.Mode)
	}
}

func TestBuildContainerConfig_ExcludesHostFields(t *testing.T) {
	cfg := &config.Config{
		Workspace: "/tmp/ws",
		Port:      8080,
		Claude: config.ClaudeConfig{
			Model:        "sonnet",
			SettingsFile: "/host/path/settings.json",
			AddDirs:      []string{"/host/extra"},
			PluginDirs:   []string{"/host/plugins"},
		},
		Git: config.GitConfig{
			AuthorName:        "Test",
			AuthorEmail:       "test@example.com",
			CredentialHelper:  "gh",
			HTTPSInsteadOfSSH: true,
		},
	}

	cc := BuildContainerConfig(cfg)

	// Verify the container config YAML doesn't contain host-only fields.
	data, err := yaml.Marshal(cc)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	content := string(data)

	// Host-only ClaudeConfig fields should not appear.
	for _, excluded := range []string{"settingsFile", "addDirs", "pluginDirs"} {
		if contains(content, excluded) {
			t.Errorf("container config should not contain %q (host-only field)", excluded)
		}
	}

	// Host-only GitConfig fields should not appear.
	for _, excluded := range []string{"credentialHelper", "httpsInsteadOfSsh"} {
		if contains(content, excluded) {
			t.Errorf("container config should not contain %q (host-only field)", excluded)
		}
	}

	// Container-relevant fields should still be present.
	if cc.Claude.Model != "sonnet" {
		t.Errorf("Claude.Model = %q, want sonnet", cc.Claude.Model)
	}
	if cc.Git.AuthorName != "Test" {
		t.Errorf("Git.AuthorName = %q, want Test", cc.Git.AuthorName)
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && len(s) >= len(substr) && stringContains(s, substr)
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestBuildContainerConfig_GitSettings(t *testing.T) {
	cfg := &config.Config{
		Workspace: "/tmp/ws",
		Port:      8080,
		Git: config.GitConfig{
			AuthorName:  "Klaus Agent",
			AuthorEmail: "klaus@example.com",
		},
	}

	cc := BuildContainerConfig(cfg)

	if cc.Git.AuthorName != "Klaus Agent" {
		t.Errorf("Git.AuthorName = %q", cc.Git.AuthorName)
	}
	if cc.Git.AuthorEmail != "klaus@example.com" {
		t.Errorf("Git.AuthorEmail = %q", cc.Git.AuthorEmail)
	}
}

func TestBuildContainerConfig_Agents(t *testing.T) {
	cfg := &config.Config{
		Workspace: "/tmp/ws",
		Port:      8080,
		Agents: map[string]config.AgentConfig{
			"reviewer": {
				Description: "Code reviewer",
				Prompt:      "Review this code.",
				Model:       "sonnet",
			},
		},
	}

	cc := BuildContainerConfig(cfg)

	if len(cc.Agents) != 1 {
		t.Fatalf("Agents length = %d, want 1", len(cc.Agents))
	}
	if cc.Agents["reviewer"].Description != "Code reviewer" {
		t.Errorf("Agents[reviewer].Description = %q", cc.Agents["reviewer"].Description)
	}
}

func TestBuildContainerConfig_NoAgentsWhenEmpty(t *testing.T) {
	cfg := &config.Config{
		Workspace: "/tmp/ws",
		Port:      8080,
	}

	cc := BuildContainerConfig(cfg)

	if cc.Agents != nil {
		t.Error("Agents should be nil when no agents configured")
	}
}

func TestRenderContainerConfig(t *testing.T) {
	paths := testPaths(t)
	r := New(paths)

	cfg := &config.Config{
		Workspace: "/home/user/project",
		Port:      9090,
		Claude: config.ClaudeConfig{
			Model:          "sonnet",
			PermissionMode: "bypassPermissions",
			MaxTurns:       25,
		},
		Git: config.GitConfig{
			AuthorName:  "Test User",
			AuthorEmail: "test@example.com",
		},
	}

	if err := r.Render(cfg); err != nil {
		t.Fatalf("Render() returned error: %v", err)
	}

	configPath := filepath.Join(paths.RenderedDir, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config.yaml not created: %v", err)
	}

	// Verify it's valid YAML.
	var cc ContainerConfig
	if err := yaml.Unmarshal(data, &cc); err != nil {
		t.Fatalf("invalid YAML: %v", err)
	}

	// Container workspace is always /workspace regardless of host path.
	if cc.Workspace != "/workspace" {
		t.Errorf("Workspace = %q, want /workspace", cc.Workspace)
	}
	// Container port is always 8080.
	if cc.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cc.Port)
	}
	if cc.Claude.Model != "sonnet" {
		t.Errorf("Claude.Model = %q, want sonnet", cc.Claude.Model)
	}
	if cc.Claude.MaxTurns != 25 {
		t.Errorf("Claude.MaxTurns = %d, want 25", cc.Claude.MaxTurns)
	}
	if cc.Git.AuthorName != "Test User" {
		t.Errorf("Git.AuthorName = %q, want Test User", cc.Git.AuthorName)
	}

	// Verify file permissions are restrictive.
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config.yaml: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config.yaml permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestRenderContainerConfig_MarshalRoundTrip(t *testing.T) {
	cfg := &config.Config{
		Workspace: "/tmp",
		Port:      8080,
		Claude: config.ClaudeConfig{
			Model:                  "opus",
			Effort:                 "high",
			IncludePartialMessages: true,
			Mode:                   "chat",
			JsonSchema:             `{"type":"object"}`,
		},
		Agents: map[string]config.AgentConfig{
			"helper": {
				Description: "A helper agent",
				Prompt:      "Help the user.",
				Tools:       []string{"read", "write"},
			},
		},
	}

	cc := BuildContainerConfig(cfg)
	data, err := yaml.Marshal(cc)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var roundTripped ContainerConfig
	if err := yaml.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if roundTripped.Claude.Model != "opus" {
		t.Errorf("Model = %q after round-trip", roundTripped.Claude.Model)
	}
	if roundTripped.Claude.Effort != "high" {
		t.Errorf("Effort = %q after round-trip", roundTripped.Claude.Effort)
	}
	if !roundTripped.Claude.IncludePartialMessages {
		t.Error("IncludePartialMessages should be true after round-trip")
	}
	if roundTripped.Claude.Mode != "chat" {
		t.Errorf("Mode = %q after round-trip, want chat", roundTripped.Claude.Mode)
	}
	if len(roundTripped.Agents) != 1 {
		t.Errorf("Agents length = %d after round-trip", len(roundTripped.Agents))
	}
}
