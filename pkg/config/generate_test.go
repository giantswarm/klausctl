package config

import (
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

	if cfg.Personality != "gsoci.azurecr.io/giantswarm/klaus-personalities/sre:latest" {
		t.Fatalf("unexpected personality: %s", cfg.Personality)
	}
	if cfg.Image != "gsoci.azurecr.io/giantswarm/klaus-go:latest" {
		t.Fatalf("unexpected image: %s", cfg.Image)
	}
	if cfg.Port != 8080 {
		t.Fatalf("unexpected port: %d", cfg.Port)
	}
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].Repository != "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-platform" {
		t.Fatalf("unexpected plugins: %+v", cfg.Plugins)
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
