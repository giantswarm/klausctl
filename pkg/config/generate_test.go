package config

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateInstanceConfig(t *testing.T) {
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	paths := &Paths{
		ConfigDir:        base,
		InstancesDir:     filepath.Join(base, "instances"),
		PluginsDir:       filepath.Join(base, "plugins"),
		PersonalitiesDir: filepath.Join(base, "personalities"),
	}

	cfg, err := GenerateInstanceConfig(paths, CreateOptions{
		Name:        "dev",
		Workspace:   workspace,
		Personality: "sre",
		Toolchain:   "go",
		Plugins:     []string{"gs-platform"},
	})
	if err != nil {
		t.Fatalf("GenerateInstanceConfig() returned error: %v", err)
	}

	if cfg.Personality != "gsoci.azurecr.io/giantswarm/klaus-personalities/sre" {
		t.Fatalf("unexpected personality: %s", cfg.Personality)
	}
	if cfg.Image != "gsoci.azurecr.io/giantswarm/klaus-toolchains/go" {
		t.Fatalf("unexpected image: %s", cfg.Image)
	}
	if cfg.Toolchain != "gsoci.azurecr.io/giantswarm/klaus-toolchains/go" {
		t.Fatalf("unexpected toolchain: %s", cfg.Toolchain)
	}
	if cfg.Port != 8080 {
		t.Fatalf("unexpected port: %d", cfg.Port)
	}
	if len(cfg.Plugins) != 1 {
		t.Fatalf("unexpected plugins count: %+v", cfg.Plugins)
	}
	if cfg.Plugins[0].Repository != "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-platform" {
		t.Fatalf("unexpected plugin repository: %s", cfg.Plugins[0].Repository)
	}
	if cfg.Plugins[0].Tag != "" {
		t.Fatalf("plugin tag should be empty (resolved at start time), got %s", cfg.Plugins[0].Tag)
	}
}

func TestGenerateInstanceConfig_PortConflict(t *testing.T) {
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	conflictInstance := filepath.Join(base, "instances", "other")
	if err := os.MkdirAll(conflictInstance, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conflictInstance, "config.yaml"), []byte("workspace: /tmp\nport: 9090\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths := &Paths{
		ConfigDir:        base,
		InstancesDir:     filepath.Join(base, "instances"),
		PluginsDir:       filepath.Join(base, "plugins"),
		PersonalitiesDir: filepath.Join(base, "personalities"),
	}

	_, err := GenerateInstanceConfig(paths, CreateOptions{
		Name:      "dev",
		Workspace: workspace,
		Port:      9090,
	})
	if err == nil {
		t.Fatal("expected error for conflicting explicit port")
	}
}

func TestGenerateInstanceConfig_ResolvedPersonalityMergesPlugins(t *testing.T) {
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	paths := &Paths{
		ConfigDir:        base,
		InstancesDir:     filepath.Join(base, "instances"),
		PluginsDir:       filepath.Join(base, "plugins"),
		PersonalitiesDir: filepath.Join(base, "personalities"),
	}

	cfg, err := GenerateInstanceConfig(paths, CreateOptions{
		Name:        "dev",
		Workspace:   workspace,
		Personality: "sre",
		Plugins:     []string{"custom"},
		Context:     context.Background(),
		ResolvePersonality: func(_ context.Context, _ string, _ io.Writer) (*ResolvedPersonality, error) {
			return &ResolvedPersonality{
				Image: "gsoci.azurecr.io/giantswarm/klaus-personality-image:latest",
				Plugins: []Plugin{
					{Repository: "gsoci.azurecr.io/giantswarm/klaus-plugins/base", Tag: "latest"},
				},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("GenerateInstanceConfig() returned error: %v", err)
	}

	if cfg.Image != "gsoci.azurecr.io/giantswarm/klaus-personality-image:latest" {
		t.Fatalf("expected personality image override, got %s", cfg.Image)
	}

	if len(cfg.Plugins) != 2 {
		t.Fatalf("expected merged plugins, got %+v", cfg.Plugins)
	}
}

func TestNextAvailablePort(t *testing.T) {
	base := t.TempDir()
	instDir := filepath.Join(base, "instances", "one")
	if err := os.MkdirAll(instDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instDir, "config.yaml"), []byte("workspace: /tmp\nport: 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths := &Paths{
		ConfigDir:        base,
		InstancesDir:     filepath.Join(base, "instances"),
		PluginsDir:       filepath.Join(base, "plugins"),
		PersonalitiesDir: filepath.Join(base, "personalities"),
	}

	port, err := NextAvailablePort(paths, 8080)
	if err != nil {
		t.Fatalf("NextAvailablePort() returned error: %v", err)
	}
	if port != 8081 {
		t.Fatalf("NextAvailablePort() = %d, want 8081", port)
	}
}

func TestGenerateInstanceConfig_Overrides(t *testing.T) {
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	paths := &Paths{
		ConfigDir:        base,
		InstancesDir:     filepath.Join(base, "instances"),
		PluginsDir:       filepath.Join(base, "plugins"),
		PersonalitiesDir: filepath.Join(base, "personalities"),
	}

	tests := []struct {
		name    string
		opts    func() CreateOptions
		check   func(t *testing.T, cfg *Config)
		wantErr bool
	}{
		{
			name: "envVars sets environment variables",
			opts: func() CreateOptions {
				return CreateOptions{
					Name: "test", Workspace: workspace,
					EnvVars: map[string]string{"GITHUB_TOKEN": "tok-123", "MY_VAR": "hello"},
				}
			},
			check: func(t *testing.T, cfg *Config) {
				if cfg.EnvVars["GITHUB_TOKEN"] != "tok-123" {
					t.Errorf("expected GITHUB_TOKEN=tok-123, got %q", cfg.EnvVars["GITHUB_TOKEN"])
				}
				if cfg.EnvVars["MY_VAR"] != "hello" {
					t.Errorf("expected MY_VAR=hello, got %q", cfg.EnvVars["MY_VAR"])
				}
			},
		},
		{
			name: "envForward appends forwarded vars",
			opts: func() CreateOptions {
				return CreateOptions{
					Name: "test", Workspace: workspace,
					EnvForward: []string{"SSH_AUTH_SOCK", "HOME"},
				}
			},
			check: func(t *testing.T, cfg *Config) {
				if len(cfg.EnvForward) != 2 {
					t.Fatalf("expected 2 envForward entries, got %d", len(cfg.EnvForward))
				}
				if cfg.EnvForward[0] != "HOME" || cfg.EnvForward[1] != "SSH_AUTH_SOCK" {
					t.Errorf("unexpected envForward: %v", cfg.EnvForward)
				}
			},
		},
		{
			name: "envForward deduplicates entries",
			opts: func() CreateOptions {
				return CreateOptions{
					Name: "test", Workspace: workspace,
					EnvForward: []string{"HOME", "SSH_AUTH_SOCK", "HOME"},
				}
			},
			check: func(t *testing.T, cfg *Config) {
				want := []string{"HOME", "SSH_AUTH_SOCK"}
				if len(cfg.EnvForward) != len(want) {
					t.Fatalf("expected %d envForward entries, got %d: %v", len(want), len(cfg.EnvForward), cfg.EnvForward)
				}
				for i, v := range want {
					if cfg.EnvForward[i] != v {
						t.Errorf("envForward[%d] = %q, want %q", i, cfg.EnvForward[i], v)
					}
				}
			},
		},
		{
			name: "mcpServers sets MCP server config",
			opts: func() CreateOptions {
				return CreateOptions{
					Name: "test", Workspace: workspace,
					McpServers: map[string]any{
						"github": map[string]any{"type": "http", "url": "https://api.example.com/mcp/"},
					},
				}
			},
			check: func(t *testing.T, cfg *Config) {
				if cfg.McpServers == nil {
					t.Fatal("mcpServers is nil")
				}
				gh, ok := cfg.McpServers["github"]
				if !ok {
					t.Fatal("expected 'github' key in mcpServers")
				}
				m := gh.(map[string]any)
				if m["type"] != "http" {
					t.Errorf("expected type=http, got %v", m["type"])
				}
			},
		},
		{
			name: "maxBudgetUsd sets budget",
			opts: func() CreateOptions {
				b := float64(10)
				return CreateOptions{
					Name: "test", Workspace: workspace,
					MaxBudgetUSD: &b,
				}
			},
			check: func(t *testing.T, cfg *Config) {
				if cfg.Claude.MaxBudgetUSD != 10 {
					t.Errorf("expected maxBudgetUsd=10, got %f", cfg.Claude.MaxBudgetUSD)
				}
			},
		},
		{
			name: "maxBudgetUsd zero explicitly removes limit",
			opts: func() CreateOptions {
				b := float64(0)
				return CreateOptions{
					Name: "test", Workspace: workspace,
					MaxBudgetUSD: &b,
				}
			},
			check: func(t *testing.T, cfg *Config) {
				if cfg.Claude.MaxBudgetUSD != 0 {
					t.Errorf("expected maxBudgetUsd=0, got %f", cfg.Claude.MaxBudgetUSD)
				}
			},
		},
		{
			name: "permissionMode sets mode",
			opts: func() CreateOptions {
				return CreateOptions{
					Name: "test", Workspace: workspace,
					PermissionMode: "dontAsk",
				}
			},
			check: func(t *testing.T, cfg *Config) {
				if cfg.Claude.PermissionMode != "dontAsk" {
					t.Errorf("expected permissionMode=dontAsk, got %q", cfg.Claude.PermissionMode)
				}
			},
		},
		{
			name: "invalid permissionMode rejected by validation",
			opts: func() CreateOptions {
				return CreateOptions{
					Name: "test", Workspace: workspace,
					PermissionMode: "invalid",
				}
			},
			wantErr: true,
		},
		{
			name: "model sets Claude model",
			opts: func() CreateOptions {
				return CreateOptions{
					Name: "test", Workspace: workspace,
					Model: "opus",
				}
			},
			check: func(t *testing.T, cfg *Config) {
				if cfg.Claude.Model != "opus" {
					t.Errorf("expected model=opus, got %q", cfg.Claude.Model)
				}
			},
		},
		{
			name: "systemPrompt sets prompt",
			opts: func() CreateOptions {
				return CreateOptions{
					Name: "test", Workspace: workspace,
					SystemPrompt: "You are a helpful assistant.",
				}
			},
			check: func(t *testing.T, cfg *Config) {
				if cfg.Claude.SystemPrompt != "You are a helpful assistant." {
					t.Errorf("expected systemPrompt override, got %q", cfg.Claude.SystemPrompt)
				}
			},
		},
		{
			name: "all overrides combined",
			opts: func() CreateOptions {
				b := float64(5)
				return CreateOptions{
					Name: "test", Workspace: workspace,
					EnvVars:        map[string]string{"KEY": "val"},
					EnvForward:     []string{"HOME"},
					MaxBudgetUSD:   &b,
					PermissionMode: "acceptEdits",
					Model:          "sonnet",
					SystemPrompt:   "Be concise.",
				}
			},
			check: func(t *testing.T, cfg *Config) {
				if cfg.EnvVars["KEY"] != "val" {
					t.Error("envVars not applied")
				}
				if len(cfg.EnvForward) != 1 || cfg.EnvForward[0] != "HOME" {
					t.Error("envForward not applied")
				}
				if cfg.Claude.MaxBudgetUSD != 5 {
					t.Error("maxBudgetUsd not applied")
				}
				if cfg.Claude.PermissionMode != "acceptEdits" {
					t.Error("permissionMode not applied")
				}
				if cfg.Claude.Model != "sonnet" {
					t.Error("model not applied")
				}
				if cfg.Claude.SystemPrompt != "Be concise." {
					t.Error("systemPrompt not applied")
				}
			},
		},
		{
			name: "no overrides leaves defaults untouched",
			opts: func() CreateOptions {
				return CreateOptions{Name: "test", Workspace: workspace}
			},
			check: func(t *testing.T, cfg *Config) {
				if cfg.Claude.PermissionMode != "bypassPermissions" {
					t.Errorf("default permissionMode changed to %q", cfg.Claude.PermissionMode)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := GenerateInstanceConfig(paths, tt.opts())

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestParsePluginRef(t *testing.T) {
	p := ParsePluginRef("gs-platform:v1.2.0")
	if p.Repository != "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-platform" {
		t.Fatalf("unexpected repository: %s", p.Repository)
	}
	if p.Tag != "v1.2.0" {
		t.Fatalf("unexpected tag: %s", p.Tag)
	}
}
