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
	if cfg.Image != "gsoci.azurecr.io/giantswarm/klaus-go" {
		t.Fatalf("unexpected image: %s", cfg.Image)
	}
	if cfg.Toolchain != "gsoci.azurecr.io/giantswarm/klaus-go" {
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

func TestParsePluginRef(t *testing.T) {
	p := ParsePluginRef("gs-platform:v1.2.0")
	if p.Repository != "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-platform" {
		t.Fatalf("unexpected repository: %s", p.Repository)
	}
	if p.Tag != "v1.2.0" {
		t.Fatalf("unexpected tag: %s", p.Tag)
	}
}
