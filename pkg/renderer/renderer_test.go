package renderer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/klausctl/pkg/config"
)

func testPaths(t *testing.T) *config.Paths {
	t.Helper()
	dir := t.TempDir()
	return &config.Paths{
		ConfigDir:     dir,
		RenderedDir:   filepath.Join(dir, "rendered"),
		ExtensionsDir: filepath.Join(dir, "rendered", "extensions"),
	}
}

func TestRenderSkills(t *testing.T) {
	paths := testPaths(t)
	r := New(paths)

	cfg := &config.Config{
		Workspace: "/tmp",
		Port:      8080,
		Skills: map[string]config.Skill{
			"test-skill": {
				Description: "A test skill",
				Content:     "This is the skill content.\n",
			},
		},
	}

	if err := r.Render(cfg); err != nil {
		t.Fatalf("Render() returned error: %v", err)
	}

	skillPath := filepath.Join(paths.ExtensionsDir, ".claude", "skills", "test-skill", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("skill file not created: %v", err)
	}

	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		t.Error("skill file should start with YAML frontmatter delimiter")
	}
	if !strings.Contains(content, "description:") {
		t.Error("skill file should contain description in frontmatter")
	}
	if !strings.Contains(content, "A test skill") {
		t.Error("skill file should contain the description text")
	}
	if !strings.Contains(content, "This is the skill content.") {
		t.Error("skill file should contain the content")
	}
}

func TestRenderSkillFrontmatter(t *testing.T) {
	skill := config.Skill{
		Description:            "Test description with 'quotes'",
		Content:                "Content body\n",
		DisableModelInvocation: true,
		UserInvocable:          true,
		AllowedTools:           "Bash",
		Model:                  "sonnet",
	}

	content, err := renderSkillContent(skill)
	if err != nil {
		t.Fatalf("renderSkillContent() returned error: %v", err)
	}

	if !strings.HasPrefix(content, "---\n") {
		t.Error("should start with frontmatter delimiter")
	}
	if !strings.Contains(content, "description:") {
		t.Error("should contain description")
	}
	if !strings.Contains(content, "disableModelInvocation: true") {
		t.Error("should contain disableModelInvocation")
	}
	if !strings.Contains(content, "userInvocable: true") {
		t.Error("should contain userInvocable")
	}
	if !strings.Contains(content, "allowedTools:") {
		t.Error("should contain allowedTools")
	}
	if !strings.Contains(content, "model:") {
		t.Error("should contain model")
	}
	if !strings.HasSuffix(content, "\n") {
		t.Error("should end with newline")
	}
}

func TestRenderAgentFiles(t *testing.T) {
	paths := testPaths(t)
	r := New(paths)

	cfg := &config.Config{
		Workspace: "/tmp",
		Port:      8080,
		AgentFiles: map[string]config.AgentFile{
			"reviewer": {Content: "You are a code reviewer.\n"},
		},
	}

	if err := r.Render(cfg); err != nil {
		t.Fatalf("Render() returned error: %v", err)
	}

	agentPath := filepath.Join(paths.ExtensionsDir, ".claude", "agents", "reviewer.md")
	data, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("agent file not created: %v", err)
	}

	if string(data) != "You are a code reviewer.\n" {
		t.Errorf("agent content = %q, want %q", string(data), "You are a code reviewer.\n")
	}
}

func TestRenderMCPConfig(t *testing.T) {
	paths := testPaths(t)
	r := New(paths)

	cfg := &config.Config{
		Workspace: "/tmp",
		Port:      8080,
		McpServers: map[string]any{
			"github": map[string]any{
				"type": "http",
				"url":  "https://example.com/mcp",
			},
		},
	}

	if err := r.Render(cfg); err != nil {
		t.Fatalf("Render() returned error: %v", err)
	}

	mcpPath := filepath.Join(paths.RenderedDir, "mcp-config.json")
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("MCP config not created: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	servers, ok := result["mcpServers"]
	if !ok {
		t.Fatal("missing mcpServers key")
	}

	serversMap, ok := servers.(map[string]any)
	if !ok {
		t.Fatal("mcpServers is not a map")
	}

	if _, ok := serversMap["github"]; !ok {
		t.Error("missing github server entry")
	}
}

func TestRenderSettings(t *testing.T) {
	paths := testPaths(t)
	r := New(paths)

	cfg := &config.Config{
		Workspace: "/tmp",
		Port:      8080,
		Hooks: map[string][]config.HookMatcher{
			"PreToolUse": {
				{
					Matcher: "Bash",
					Hooks: []config.Hook{
						{Type: "command", Command: "/etc/klaus/hooks/check.sh"},
					},
				},
			},
		},
	}

	if err := r.Render(cfg); err != nil {
		t.Fatalf("Render() returned error: %v", err)
	}

	settingsPath := filepath.Join(paths.RenderedDir, "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("settings file not created: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if _, ok := result["hooks"]; !ok {
		t.Error("missing hooks key")
	}
}

func TestRenderHookScripts(t *testing.T) {
	paths := testPaths(t)
	r := New(paths)

	cfg := &config.Config{
		Workspace: "/tmp",
		Port:      8080,
		HookScripts: map[string]string{
			"check.sh": "#!/bin/bash\nexit 0\n",
		},
	}

	if err := r.Render(cfg); err != nil {
		t.Fatalf("Render() returned error: %v", err)
	}

	scriptPath := filepath.Join(paths.RenderedDir, "hooks", "check.sh")
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("hook script not created: %v", err)
	}

	// Check executable bit.
	if info.Mode()&0o100 == 0 {
		t.Error("hook script should be executable")
	}
}

func TestRenderCleansPreviousOutput(t *testing.T) {
	paths := testPaths(t)
	r := New(paths)

	// First render creates a file.
	cfg := &config.Config{
		Workspace: "/tmp",
		Port:      8080,
		Skills: map[string]config.Skill{
			"old-skill": {Content: "old\n"},
		},
	}
	if err := r.Render(cfg); err != nil {
		t.Fatalf("first Render() returned error: %v", err)
	}

	oldPath := filepath.Join(paths.ExtensionsDir, ".claude", "skills", "old-skill", "SKILL.md")
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatal("old skill should exist after first render")
	}

	// Second render with different config should clean old output.
	cfg.Skills = map[string]config.Skill{
		"new-skill": {Content: "new\n"},
	}
	if err := r.Render(cfg); err != nil {
		t.Fatalf("second Render() returned error: %v", err)
	}

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("old skill should be removed after second render")
	}

	newPath := filepath.Join(paths.ExtensionsDir, ".claude", "skills", "new-skill", "SKILL.md")
	if _, err := os.Stat(newPath); err != nil {
		t.Error("new skill should exist after second render")
	}
}

func TestHasExtensions(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want bool
	}{
		{
			name: "no extensions",
			cfg:  &config.Config{},
			want: false,
		},
		{
			name: "with skills",
			cfg:  &config.Config{Skills: map[string]config.Skill{"s": {Content: "x"}}},
			want: true,
		},
		{
			name: "with agent files",
			cfg:  &config.Config{AgentFiles: map[string]config.AgentFile{"a": {Content: "x"}}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasExtensions(tt.cfg)
			if got != tt.want {
				t.Errorf("HasExtensions() = %v, want %v", got, tt.want)
			}
		})
	}
}
