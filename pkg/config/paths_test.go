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
	if got := filepath.Base(paths.InstanceDir); got != "default" { //nolint:goconst
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

	if custom.MusterConfigDir != paths.MusterConfigDir {
		t.Errorf("ForInstance MusterConfigDir = %q, want %q", custom.MusterConfigDir, paths.MusterConfigDir)
	}
	if custom.MusterMCPServersDir != paths.MusterMCPServersDir {
		t.Errorf("ForInstance MusterMCPServersDir = %q, want %q", custom.MusterMCPServersDir, paths.MusterMCPServersDir)
	}
	if custom.MusterPIDFile != paths.MusterPIDFile {
		t.Errorf("ForInstance MusterPIDFile = %q, want %q", custom.MusterPIDFile, paths.MusterPIDFile)
	}
	if custom.MusterPortFile != paths.MusterPortFile {
		t.Errorf("ForInstance MusterPortFile = %q, want %q", custom.MusterPortFile, paths.MusterPortFile)
	}
}

func TestDefaultPathsMuster(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	paths, err := DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths() returned error: %v", err)
	}

	base := filepath.Join(dir, "klausctl")

	if want := filepath.Join(base, "muster"); paths.MusterConfigDir != want {
		t.Errorf("MusterConfigDir = %q, want %q", paths.MusterConfigDir, want)
	}
	if want := filepath.Join(base, "muster", "mcpservers"); paths.MusterMCPServersDir != want {
		t.Errorf("MusterMCPServersDir = %q, want %q", paths.MusterMCPServersDir, want)
	}
	if want := filepath.Join(base, "muster.pid"); paths.MusterPIDFile != want {
		t.Errorf("MusterPIDFile = %q, want %q", paths.MusterPIDFile, want)
	}
	if want := filepath.Join(base, "muster.port"); paths.MusterPortFile != want {
		t.Errorf("MusterPortFile = %q, want %q", paths.MusterPortFile, want)
	}
}

func TestHasMusterConfig(t *testing.T) {
	t.Run("no directory", func(t *testing.T) {
		paths := &Paths{
			MusterMCPServersDir: filepath.Join(t.TempDir(), "nonexistent", "mcpservers"),
		}
		has, err := paths.HasMusterConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if has {
			t.Error("expected false for nonexistent directory")
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "mcpservers")
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		paths := &Paths{MusterMCPServersDir: dir}
		has, err := paths.HasMusterConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if has {
			t.Error("expected false for empty directory")
		}
	})

	t.Run("directory with non-yaml files", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "mcpservers")
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not yaml"), 0o600); err != nil {
			t.Fatal(err)
		}
		paths := &Paths{MusterMCPServersDir: dir}
		has, err := paths.HasMusterConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if has {
			t.Error("expected false when no YAML files present")
		}
	})

	t.Run("directory with yaml file", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "mcpservers")
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "pro.yaml"), []byte("kind: MCPServer\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		paths := &Paths{MusterMCPServersDir: dir}
		has, err := paths.HasMusterConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !has {
			t.Error("expected true when YAML file present")
		}
	})

	t.Run("directory with yml file", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "mcpservers")
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "klausctl.yml"), []byte("kind: MCPServer\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		paths := &Paths{MusterMCPServersDir: dir}
		has, err := paths.HasMusterConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !has {
			t.Error("expected true when YML file present")
		}
	})

	t.Run("subdirectories ignored", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "mcpservers")
		subdir := filepath.Join(dir, "subdir.yaml")
		if err := os.MkdirAll(subdir, 0o750); err != nil {
			t.Fatal(err)
		}
		paths := &Paths{MusterMCPServersDir: dir}
		has, err := paths.HasMusterConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if has {
			t.Error("expected false when only subdirectories present")
		}
	})
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

func TestDefaultPathsRespectsSourcesFileEnv(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("KLAUSCTL_SOURCES_FILE", "/etc/klaus/sources.yaml")

	paths, err := DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths() returned error: %v", err)
	}

	if paths.SourcesFile != "/etc/klaus/sources.yaml" {
		t.Errorf("SourcesFile = %q, want /etc/klaus/sources.yaml", paths.SourcesFile)
	}
}

func TestDefaultPathsSourcesFileDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("KLAUSCTL_SOURCES_FILE", "")

	paths, err := DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths() returned error: %v", err)
	}

	expected := filepath.Join(dir, "klausctl", "sources.yaml")
	if paths.SourcesFile != expected {
		t.Errorf("SourcesFile = %q, want %q", paths.SourcesFile, expected)
	}
}

func TestResolveRefs(t *testing.T) {
	r := DefaultSourceResolver()

	tests := []struct {
		name string
		fn   func(string) string
		ref  string
		want string
	}{
		{
			name: "personality short name",
			fn:   r.ResolvePersonalityRef,
			ref:  "sre",
			want: "gsoci.azurecr.io/giantswarm/klaus-personalities/sre",
		},
		{
			name: "personality short name with tag",
			fn:   r.ResolvePersonalityRef,
			ref:  "sre:v0.2.0",
			want: "gsoci.azurecr.io/giantswarm/klaus-personalities/sre:v0.2.0",
		},
		{
			name: "personality full ref unchanged",
			fn:   r.ResolvePersonalityRef,
			ref:  "custom.io/org/my-personality:v1.0.0",
			want: "custom.io/org/my-personality:v1.0.0",
		},
		{
			name: "toolchain short name with tag",
			fn:   r.ResolveToolchainRef,
			ref:  "go:v1.0.0",
			want: "gsoci.azurecr.io/giantswarm/klaus-toolchains/go:v1.0.0",
		},
		{
			name: "toolchain short name without tag",
			fn:   r.ResolveToolchainRef,
			ref:  "go",
			want: "gsoci.azurecr.io/giantswarm/klaus-toolchains/go",
		},
		{
			name: "plugin short name",
			fn:   r.ResolvePluginRef,
			ref:  "gs-platform",
			want: "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-platform",
		},
		{
			name: "plugin short name with tag",
			fn:   r.ResolvePluginRef,
			ref:  "gs-platform:v0.0.5",
			want: "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-platform:v0.0.5",
		},
		{
			name: "empty ref",
			fn:   r.ResolvePluginRef,
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
