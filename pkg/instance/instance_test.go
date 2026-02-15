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
	return &config.Paths{
		ConfigDir:    dir,
		InstanceFile: filepath.Join(dir, "instance.json"),
	}
}

func TestSaveAndLoad(t *testing.T) {
	paths := testPaths(t)

	now := time.Now()
	inst := &Instance{
		Name:        "test",
		ContainerID: "abc123",
		Runtime:     "docker",
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
