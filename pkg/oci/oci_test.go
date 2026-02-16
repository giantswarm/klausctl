package oci

import (
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
			got := buildRef(tt.plugin)
			if got != tt.want {
				t.Errorf("buildRef() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTruncateDigest(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			want:  "sha256:abcdef123456",
		},
		{
			input: "sha256:short",
			want:  "sha256:short",
		},
		{
			input: "noprefix",
			want:  "noprefix",
		},
		{
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncateDigest(tt.input)
			if got != tt.want {
				t.Errorf("truncateDigest(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
