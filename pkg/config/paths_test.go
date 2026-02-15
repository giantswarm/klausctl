package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPaths(t *testing.T) {
	paths, err := DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths() returned error: %v", err)
	}

	if paths.ConfigDir == "" {
		t.Error("ConfigDir should not be empty")
	}
	if paths.ConfigFile == "" {
		t.Error("ConfigFile should not be empty")
	}
	if filepath.Base(paths.ConfigFile) != "config.yaml" {
		t.Errorf("ConfigFile base = %q, want %q", filepath.Base(paths.ConfigFile), "config.yaml")
	}
	if filepath.Base(paths.InstanceFile) != "instance.json" {
		t.Errorf("InstanceFile base = %q, want %q", filepath.Base(paths.InstanceFile), "instance.json")
	}
}

func TestDefaultPathsRespectsXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	paths, err := DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths() returned error: %v", err)
	}

	expected := filepath.Join(dir, "klausctl")
	if paths.ConfigDir != expected {
		t.Errorf("ConfigDir = %q, want %q", paths.ConfigDir, expected)
	}
}

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "tilde prefix",
			path: "~/projects",
			want: filepath.Join(home, "projects"),
		},
		{
			name: "bare tilde",
			path: "~",
			want: home,
		},
		{
			name: "absolute path unchanged",
			path: "/tmp/test",
			want: "/tmp/test",
		},
		{
			name: "relative path unchanged",
			path: "relative/path",
			want: "relative/path",
		},
		{
			name: "tilde in middle unchanged",
			path: "/some/~path",
			want: "/some/~path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandPath(tt.path)
			if got != tt.want {
				t.Errorf("ExpandPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestEnsureDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b", "c")

	if err := EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir() returned error: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected a directory")
	}

	// Should not fail on existing directory.
	if err := EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir() on existing directory returned error: %v", err)
	}
}
