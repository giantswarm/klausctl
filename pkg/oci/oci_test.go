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

	if dirs[0] != "/mnt/plugins/plugin-a" {
		t.Errorf("dirs[0] = %q, want %q", dirs[0], "/mnt/plugins/plugin-a")
	}
	if dirs[1] != "/mnt/plugins/plugin-b" {
		t.Errorf("dirs[1] = %q, want %q", dirs[1], "/mnt/plugins/plugin-b")
	}
}

func TestPluginDirsEmpty(t *testing.T) {
	dirs := PluginDirs(nil)
	if len(dirs) != 0 {
		t.Errorf("PluginDirs(nil) returned %d dirs, want 0", len(dirs))
	}
}

func TestPullPluginsCreatesDirectories(t *testing.T) {
	dir := t.TempDir()

	plugins := []config.Plugin{
		{Repository: "example.com/org/test-plugin", Tag: "v1.0.0"},
	}

	if err := PullPlugins(plugins, dir); err != nil {
		t.Fatalf("PullPlugins() returned error: %v", err)
	}
}
