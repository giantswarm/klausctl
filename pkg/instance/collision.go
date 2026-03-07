package instance

import (
	"context"
	"errors"
	"os"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

// CollisionState describes the state of an existing instance that collides
// with a requested name.
type CollisionState int

const (
	// NoCollision means no instance with the given name exists.
	NoCollision CollisionState = iota
	// CollisionStopped means an instance directory exists but no container is running.
	CollisionStopped
	// CollisionRunning means a container for this instance is currently running.
	CollisionRunning
)

// CheckCollision determines whether an instance with the given name already
// exists and, if so, whether its container is running or stopped.
func CheckCollision(ctx context.Context, paths *config.Paths) (CollisionState, error) {
	if _, err := os.Stat(paths.InstanceDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NoCollision, nil
		}
		return NoCollision, err
	}

	// Instance directory exists. Check if a container is running.
	inst, err := Load(paths)
	if err != nil {
		// No instance state file — treat as stopped.
		return CollisionStopped, nil
	}

	rt, err := runtime.New(inst.Runtime)
	if err != nil {
		// Can't determine runtime — conservative: treat as stopped.
		return CollisionStopped, nil
	}

	status, err := rt.Status(ctx, inst.ContainerName())
	if err != nil || status == "" || status != "running" {
		return CollisionStopped, nil
	}

	return CollisionRunning, nil
}
