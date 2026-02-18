package instance

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/giantswarm/klausctl/pkg/config"
)

func testPaths(t *testing.T) *config.Paths {
	t.Helper()
	dir := t.TempDir()
	instancesDir := filepath.Join(dir, "instances")
	instanceDir := filepath.Join(instancesDir, "default")
	return &config.Paths{
		ConfigDir:    dir,
		InstancesDir: instancesDir,
		InstanceDir:  instanceDir,
		InstanceFile: filepath.Join(instanceDir, "instance.json"),
	}
}

func TestSaveAndLoad(t *testing.T) {
	paths := testPaths(t)

	now := time.Now()
	inst := &Instance{
		Name:        "test",
		ContainerID: "abc123",
		Runtime:     "docker",
		Personality: "gsoci.azurecr.io/giantswarm/klaus-personalities/sre:v1.0.0",
		Image:       "ghcr.io/giantswarm/klaus:latest",
		Port:        8080,
		Workspace:   "/tmp/test",
		StartedAt:   now,
	}

	if err := inst.Save(paths); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	loaded, err := Load(paths)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if loaded.Name != inst.Name {
		t.Errorf("Name = %q, want %q", loaded.Name, inst.Name)
	}
	if loaded.ContainerID != inst.ContainerID {
		t.Errorf("ContainerID = %q, want %q", loaded.ContainerID, inst.ContainerID)
	}
	if loaded.Runtime != inst.Runtime {
		t.Errorf("Runtime = %q, want %q", loaded.Runtime, inst.Runtime)
	}
	if loaded.Personality != inst.Personality {
		t.Errorf("Personality = %q, want %q", loaded.Personality, inst.Personality)
	}
	if loaded.Image != inst.Image {
		t.Errorf("Image = %q, want %q", loaded.Image, inst.Image)
	}
	if loaded.Port != inst.Port {
		t.Errorf("Port = %d, want %d", loaded.Port, inst.Port)
	}
	if loaded.Workspace != inst.Workspace {
		t.Errorf("Workspace = %q, want %q", loaded.Workspace, inst.Workspace)
	}
	if loaded.StartedAt.IsZero() {
		t.Error("StartedAt should not be zero after Save()")
	}
}

func TestLoadMissing(t *testing.T) {
	paths := testPaths(t)

	_, err := Load(paths)
	if err == nil {
		t.Fatal("Load() should return error for missing file")
	}
}

func TestClear(t *testing.T) {
	paths := testPaths(t)

	inst := &Instance{
		Name:        "test",
		ContainerID: "abc123",
		Runtime:     "docker",
		Image:       "test:latest",
		Port:        8080,
		Workspace:   "/tmp",
		StartedAt:   time.Now(),
	}

	if err := inst.Save(paths); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	// Verify it exists.
	if _, err := Load(paths); err != nil {
		t.Fatalf("Load() should succeed after Save(): %v", err)
	}

	if err := Clear(paths); err != nil {
		t.Fatalf("Clear() returned error: %v", err)
	}

	// Verify it's gone.
	if _, err := Load(paths); err == nil {
		t.Fatal("Load() should fail after Clear()")
	}
}

func TestClearMissingIsNotError(t *testing.T) {
	paths := testPaths(t)

	// Clear on a non-existent file should not return an error.
	if err := Clear(paths); err != nil {
		t.Fatalf("Clear() on missing file returned error: %v", err)
	}
}

func TestContainerName(t *testing.T) {
	inst := &Instance{Name: "default"}
	if got := inst.ContainerName(); got != "klausctl-default" {
		t.Errorf("ContainerName() = %q, want %q", got, "klausctl-default")
	}

	inst = &Instance{Name: "custom"}
	if got := inst.ContainerName(); got != "klausctl-custom" {
		t.Errorf("ContainerName() = %q, want %q", got, "klausctl-custom")
	}
}

func TestLoadAll(t *testing.T) {
	paths := testPaths(t)

	first := &Instance{
		Name:        "default",
		ContainerID: "id-1",
		Runtime:     "docker",
		Image:       "image:latest",
		Port:        8080,
		Workspace:   "/tmp/a",
		StartedAt:   time.Now(),
	}
	if err := first.Save(paths.ForInstance("default")); err != nil {
		t.Fatalf("saving first instance: %v", err)
	}

	second := &Instance{
		Name:        "dev",
		ContainerID: "id-2",
		Runtime:     "docker",
		Image:       "image:latest",
		Port:        8081,
		Workspace:   "/tmp/b",
		StartedAt:   time.Now(),
	}
	if err := second.Save(paths.ForInstance("dev")); err != nil {
		t.Fatalf("saving second instance: %v", err)
	}

	instances, err := LoadAll(paths)
	if err != nil {
		t.Fatalf("LoadAll() returned error: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("LoadAll() returned %d instances, want 2", len(instances))
	}
}
