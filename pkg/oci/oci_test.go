package oci

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/giantswarm/klausctl/pkg/config"
)

func TestShortPluginName(t *testing.T) {
	tests := []struct {
		repository string
		want       string
	}{
		{
			repository: "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-platform",
			want:       "gs-platform",
		},
		{
			repository: "example.com/plugin",
			want:       "plugin",
		},
		{
			repository: "simple",
			want:       "simple",
		},
		{
			repository: "",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.repository, func(t *testing.T) {
			got := ShortPluginName(tt.repository)
			if got != tt.want {
				t.Errorf("ShortPluginName(%q) = %q, want %q", tt.repository, got, tt.want)
			}
		})
	}
}

func TestPluginDirs(t *testing.T) {
	plugins := []config.Plugin{
		{Repository: "example.com/org/plugin-a", Tag: "v1.0.0"},
		{Repository: "example.com/org/plugin-b", Tag: "v2.0.0"},
	}

	dirs := PluginDirs(plugins)
	if len(dirs) != 2 {
		t.Fatalf("PluginDirs() returned %d dirs, want 2", len(dirs))
	}

	if dirs[0] != "/var/lib/klaus/plugins/plugin-a" {
		t.Errorf("dirs[0] = %q, want %q", dirs[0], "/var/lib/klaus/plugins/plugin-a")
	}
	if dirs[1] != "/var/lib/klaus/plugins/plugin-b" {
		t.Errorf("dirs[1] = %q, want %q", dirs[1], "/var/lib/klaus/plugins/plugin-b")
	}
}

func TestPluginDirsEmpty(t *testing.T) {
	dirs := PluginDirs(nil)
	if len(dirs) != 0 {
		t.Errorf("PluginDirs(nil) returned %d dirs, want 0", len(dirs))
	}
}

func TestBuildRef(t *testing.T) {
	tests := []struct {
		name   string
		plugin config.Plugin
		want   string
	}{
		{
			name:   "tag",
			plugin: config.Plugin{Repository: "example.com/plugin", Tag: "v1.0.0"},
			want:   "example.com/plugin:v1.0.0",
		},
		{
			name:   "digest",
			plugin: config.Plugin{Repository: "example.com/plugin", Digest: "sha256:abc123"},
			want:   "example.com/plugin@sha256:abc123",
		},
		{
			name:   "digest takes precedence over tag",
			plugin: config.Plugin{Repository: "example.com/plugin", Tag: "v1.0.0", Digest: "sha256:abc123"},
			want:   "example.com/plugin@sha256:abc123",
		},
		{
			name:   "no tag or digest",
			plugin: config.Plugin{Repository: "example.com/plugin"},
			want:   "example.com/plugin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildRef(tt.plugin)
			if got != tt.want {
				t.Errorf("BuildRef() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToolchainRegistryRef(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{
			name: "go",
			want: "gsoci.azurecr.io/giantswarm/klaus-go",
		},
		{
			name: "python",
			want: "gsoci.azurecr.io/giantswarm/klaus-python",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToolchainRegistryRef(tt.name)
			if got != tt.want {
				t.Errorf("ToolchainRegistryRef(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestPluginFromReference(t *testing.T) {
	ref := PluginReference{
		Repository: "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-platform",
		Tag:        "v1.0.0",
	}

	p := PluginFromReference(ref)
	if p.Repository != ref.Repository {
		t.Errorf("Repository = %q, want %q", p.Repository, ref.Repository)
	}
	if p.Tag != ref.Tag {
		t.Errorf("Tag = %q, want %q", p.Tag, ref.Tag)
	}
	if p.Digest != "" {
		t.Errorf("Digest = %q, want empty", p.Digest)
	}
}

func TestPluginFromReferenceWithDigest(t *testing.T) {
	ref := PluginReference{
		Repository: "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base",
		Digest:     "sha256:abc123",
	}

	p := PluginFromReference(ref)
	if p.Digest != ref.Digest {
		t.Errorf("Digest = %q, want %q", p.Digest, ref.Digest)
	}
}

func TestMergePluginsUserWins(t *testing.T) {
	personalityPlugins := []PluginReference{
		{Repository: "example.com/plugin-a", Tag: "v1.0.0"},
		{Repository: "example.com/plugin-b", Tag: "v1.0.0"},
	}
	userPlugins := []config.Plugin{
		{Repository: "example.com/plugin-a", Tag: "v2.0.0"},
	}

	merged := MergePlugins(personalityPlugins, userPlugins)

	if len(merged) != 2 {
		t.Fatalf("len(merged) = %d, want 2", len(merged))
	}
	if merged[0].Repository != "example.com/plugin-a" || merged[0].Tag != "v2.0.0" {
		t.Errorf("merged[0] = %+v, want user version (v2.0.0)", merged[0])
	}
	if merged[1].Repository != "example.com/plugin-b" || merged[1].Tag != "v1.0.0" {
		t.Errorf("merged[1] = %+v, want personality plugin-b", merged[1])
	}
}

func TestMergePluginsNoOverlap(t *testing.T) {
	personalityPlugins := []PluginReference{
		{Repository: "example.com/plugin-a", Tag: "v1.0.0"},
	}
	userPlugins := []config.Plugin{
		{Repository: "example.com/plugin-b", Tag: "v2.0.0"},
	}

	merged := MergePlugins(personalityPlugins, userPlugins)

	if len(merged) != 2 {
		t.Fatalf("len(merged) = %d, want 2", len(merged))
	}
	if merged[0].Repository != "example.com/plugin-b" {
		t.Errorf("merged[0] should be user plugin, got %q", merged[0].Repository)
	}
	if merged[1].Repository != "example.com/plugin-a" {
		t.Errorf("merged[1] should be personality plugin, got %q", merged[1].Repository)
	}
}

func TestMergePluginsEmptyPersonality(t *testing.T) {
	userPlugins := []config.Plugin{
		{Repository: "example.com/plugin-a", Tag: "v1.0.0"},
	}

	merged := MergePlugins(nil, userPlugins)

	if len(merged) != 1 {
		t.Fatalf("len(merged) = %d, want 1", len(merged))
	}
	if merged[0].Repository != "example.com/plugin-a" {
		t.Errorf("merged[0] = %+v, want user plugin", merged[0])
	}
}

func TestMergePluginsEmptyUser(t *testing.T) {
	personalityPlugins := []PluginReference{
		{Repository: "example.com/plugin-a", Tag: "v1.0.0"},
		{Repository: "example.com/plugin-b", Tag: "v2.0.0"},
	}

	merged := MergePlugins(personalityPlugins, nil)

	if len(merged) != 2 {
		t.Fatalf("len(merged) = %d, want 2", len(merged))
	}
}

func TestMergePluginsBothEmpty(t *testing.T) {
	merged := MergePlugins(nil, nil)
	if len(merged) != 0 {
		t.Fatalf("len(merged) = %d, want 0", len(merged))
	}
}

func TestMergePluginsDeduplicatesPersonality(t *testing.T) {
	personalityPlugins := []PluginReference{
		{Repository: "example.com/plugin-a", Tag: "v1.0.0"},
		{Repository: "example.com/plugin-a", Tag: "v1.1.0"},
	}

	merged := MergePlugins(personalityPlugins, nil)

	if len(merged) != 1 {
		t.Fatalf("len(merged) = %d, want 1 (dedup within personality)", len(merged))
	}
	if merged[0].Tag != "v1.0.0" {
		t.Errorf("merged[0].Tag = %q, want first occurrence v1.0.0", merged[0].Tag)
	}
}

func TestLoadPersonalitySpec(t *testing.T) {
	dir := t.TempDir()
	specContent := `
description: SRE personality
image: gsoci.azurecr.io/giantswarm/klaus-go:1.0.0
plugins:
  - repository: gsoci.azurecr.io/giantswarm/klaus-plugins/gs-platform
    tag: v1.0.0
  - repository: gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base
    tag: v0.5.0
`
	if err := os.WriteFile(filepath.Join(dir, "personality.yaml"), []byte(specContent), 0o644); err != nil {
		t.Fatal(err)
	}

	spec, err := LoadPersonalitySpec(dir)
	if err != nil {
		t.Fatalf("LoadPersonalitySpec() error: %v", err)
	}

	if spec.Description != "SRE personality" {
		t.Errorf("Description = %q, want %q", spec.Description, "SRE personality")
	}
	if spec.Image != "gsoci.azurecr.io/giantswarm/klaus-go:1.0.0" {
		t.Errorf("Image = %q, want toolchain image", spec.Image)
	}
	if len(spec.Plugins) != 2 {
		t.Fatalf("len(Plugins) = %d, want 2", len(spec.Plugins))
	}
	if spec.Plugins[0].Repository != "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-platform" {
		t.Errorf("Plugins[0].Repository = %q", spec.Plugins[0].Repository)
	}
}

func TestLoadPersonalitySpecMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadPersonalitySpec(dir)
	if err == nil {
		t.Fatal("LoadPersonalitySpec() should error when personality.yaml is missing")
	}
}

func TestHasSOULFile(t *testing.T) {
	dir := t.TempDir()

	if HasSOULFile(dir) {
		t.Error("HasSOULFile() = true for empty dir, want false")
	}

	if err := os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("# Identity"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !HasSOULFile(dir) {
		t.Error("HasSOULFile() = false after creating SOUL.md, want true")
	}
}

func TestNewDefaultClient(t *testing.T) {
	client := NewDefaultClient()
	if client == nil {
		t.Fatal("NewDefaultClient() returned nil")
	}
}

func TestNewDefaultClientWithOpts(t *testing.T) {
	client := NewDefaultClient(WithPlainHTTP(true))
	if client == nil {
		t.Fatal("NewDefaultClient(WithPlainHTTP(true)) returned nil")
	}
}

func TestRegistryAuthEnvVar(t *testing.T) {
	if RegistryAuthEnvVar != "KLAUSCTL_REGISTRY_AUTH" {
		t.Errorf("RegistryAuthEnvVar = %q, want %q", RegistryAuthEnvVar, "KLAUSCTL_REGISTRY_AUTH")
	}
}

func TestToolchainRegistryRefFullPath(t *testing.T) {
	full := "gsoci.azurecr.io/giantswarm/klaus-go"
	got := ToolchainRegistryRef(full)
	if got != full {
		t.Errorf("ToolchainRegistryRef(%q) = %q, want %q (should return as-is)", full, got, full)
	}
}
