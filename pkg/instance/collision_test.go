package instance

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/giantswarm/klausctl/pkg/config"
)

func TestCheckCollisionNoInstance(t *testing.T) {
	dir := t.TempDir()
	paths := &config.Paths{
		InstanceDir:  filepath.Join(dir, "nonexistent"),
		InstanceFile: filepath.Join(dir, "nonexistent", "instance.json"),
	}

	state, err := CheckCollision(context.Background(), paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != NoCollision {
		t.Fatalf("expected NoCollision, got %d", state)
	}
}

func TestCheckCollisionStoppedNoStateFile(t *testing.T) {
	dir := t.TempDir()
	instDir := filepath.Join(dir, "myinst")
	if err := os.MkdirAll(instDir, 0o750); err != nil {
		t.Fatal(err)
	}

	paths := &config.Paths{
		InstanceDir:  instDir,
		InstanceFile: filepath.Join(instDir, "instance.json"),
	}

	state, err := CheckCollision(context.Background(), paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != CollisionStopped {
		t.Fatalf("expected CollisionStopped, got %d", state)
	}
}

func TestCheckCollisionStoppedWithStaleState(t *testing.T) {
	dir := t.TempDir()
	instDir := filepath.Join(dir, "myinst")
	if err := os.MkdirAll(instDir, 0o750); err != nil {
		t.Fatal(err)
	}

	// Write instance state with an invalid runtime so Status() can't check a real container.
	stateData := `{"name":"myinst","containerID":"abc123","runtime":"nonexistent","port":8080}`
	if err := os.WriteFile(filepath.Join(instDir, "instance.json"), []byte(stateData), 0o600); err != nil {
		t.Fatal(err)
	}

	paths := &config.Paths{
		InstanceDir:  instDir,
		InstanceFile: filepath.Join(instDir, "instance.json"),
	}

	state, err := CheckCollision(context.Background(), paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With an invalid runtime, we can't determine status, so treat as stopped.
	if state != CollisionStopped {
		t.Fatalf("expected CollisionStopped, got %d", state)
	}
}
