package config

import (
	"os"
	"path/filepath"
	"strings"
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
	if got := filepath.Base(paths.InstancesDir); got != "instances" {
		t.Errorf("InstancesDir base = %q, want %q", got, "instances")
	}
	if got := filepath.Base(paths.InstanceDir); got != "default" {
		t.Errorf("InstanceDir base = %q, want %q", got, "default")
	}
	if !strings.Contains(paths.InstanceFile, filepath.Join("instances", "default")) {
		t.Errorf("InstanceFile = %q, expected scoped default path", paths.InstanceFile)
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

func TestForInstance(t *testing.T) {
	paths, err := DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths() returned error: %v", err)
	}

	custom := paths.ForInstance("dev")
	if got := filepath.Base(custom.InstanceDir); got != "dev" {
		t.Fatalf("InstanceDir base = %q, want dev", got)
	}
	if got := filepath.Base(custom.ConfigFile); got != "config.yaml" {
		t.Fatalf("ConfigFile base = %q, want config.yaml", got)
	}
	if got := filepath.Base(custom.InstanceFile); got != "instance.json" {
		t.Fatalf("InstanceFile base = %q, want instance.json", got)
	}
}

func TestValidateInstanceName(t *testing.T) {
	valid := []string{"default", "dev-1", "A1", "x"}
	for _, name := range valid {
		if err := ValidateInstanceName(name); err != nil {
			t.Fatalf("ValidateInstanceName(%q) returned error: %v", name, err)
		}
	}

	invalid := []string{"", "1dev", "-dev", "dev_", strings.Repeat("a", 64), "dev-"}
	for _, name := range invalid {
		if err := ValidateInstanceName(name); err == nil {
			t.Fatalf("ValidateInstanceName(%q) expected error", name)
		}
	}
}

func TestResolveRefs(t *testing.T) {
	tests := []struct {
		name string
		fn   func(string) string
		ref  string
		want string
	}{
		{
			name: "personality short name",
			fn:   ResolvePersonalityRef,
			ref:  "sre",
			want: "gsoci.azurecr.io/giantswarm/klaus-personalities/sre",
		},
		{
			name: "personality short name with tag",
			fn:   ResolvePersonalityRef,
			ref:  "sre:v0.2.0",
			want: "gsoci.azurecr.io/giantswarm/klaus-personalities/sre:v0.2.0",
		},
		{
			name: "personality full ref unchanged",
			fn:   ResolvePersonalityRef,
			ref:  "custom.io/org/my-personality:v1.0.0",
			want: "custom.io/org/my-personality:v1.0.0",
		},
		{
			name: "toolchain short name with tag",
			fn:   ResolveToolchainRef,
			ref:  "go:v1.0.0",
			want: "gsoci.azurecr.io/giantswarm/klaus-toolchains/go:v1.0.0",
		},
		{
			name: "toolchain short name without tag",
			fn:   ResolveToolchainRef,
			ref:  "go",
			want: "gsoci.azurecr.io/giantswarm/klaus-toolchains/go",
		},
		{
			name: "plugin short name",
			fn:   ResolvePluginRef,
			ref:  "gs-platform",
			want: "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-platform",
		},
		{
			name: "plugin short name with tag",
			fn:   ResolvePluginRef,
			ref:  "gs-platform:v0.0.5",
			want: "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-platform:v0.0.5",
		},
		{
			name: "empty ref",
			fn:   ResolvePluginRef,
			ref:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn(tt.ref)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
