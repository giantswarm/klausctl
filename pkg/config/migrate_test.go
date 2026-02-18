package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateLayout(t *testing.T) {
	base := t.TempDir()
	paths := &Paths{
		ConfigDir:        base,
		InstancesDir:     filepath.Join(base, "instances"),
		PluginsDir:       filepath.Join(base, "plugins"),
		PersonalitiesDir: filepath.Join(base, "personalities"),
	}

	if err := os.WriteFile(filepath.Join(base, "config.yaml"), []byte("workspace: /tmp\nport: 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "instance.json"), []byte(`{"name":"default","port":8080}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDir(filepath.Join(base, "rendered")); err != nil {
		t.Fatal(err)
	}

	if err := MigrateLayout(paths); err != nil {
		t.Fatalf("MigrateLayout() returned error: %v", err)
	}

	def := paths.ForInstance("default")
	if _, err := os.Stat(def.ConfigFile); err != nil {
		t.Fatalf("expected migrated config at %s: %v", def.ConfigFile, err)
	}
	if _, err := os.Stat(def.InstanceFile); err != nil {
		t.Fatalf("expected migrated instance at %s: %v", def.InstanceFile, err)
	}
	if _, err := os.Stat(def.RenderedDir); err != nil {
		t.Fatalf("expected migrated rendered dir at %s: %v", def.RenderedDir, err)
	}
}

func TestMigrateLayoutIdempotent(t *testing.T) {
	base := t.TempDir()
	paths := &Paths{
		ConfigDir:        base,
		InstancesDir:     filepath.Join(base, "instances"),
		PluginsDir:       filepath.Join(base, "plugins"),
		PersonalitiesDir: filepath.Join(base, "personalities"),
	}

	if err := MigrateLayout(paths); err != nil {
		t.Fatalf("first MigrateLayout() returned error: %v", err)
	}
	if err := MigrateLayout(paths); err != nil {
		t.Fatalf("second MigrateLayout() returned error: %v", err)
	}
}
