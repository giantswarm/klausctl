// Package runtime provides a container runtime abstraction for Docker and Podman.
// Both runtimes share a compatible CLI interface, so a single implementation handles
// both via os/exec.
package runtime

import (
	"context"
	"fmt"
	"time"
)

// Runtime is the interface for managing container lifecycle operations.
type Runtime interface {
	// Name returns the runtime name ("docker" or "podman").
	Name() string
	// Run starts a new container and returns its ID.
	Run(ctx context.Context, opts RunOptions) (string, error)
	// Stop stops a running container.
	Stop(ctx context.Context, name string) error
	// Remove removes a container.
	Remove(ctx context.Context, name string) error
	// Status returns the container status ("running", "exited", "created", etc.)
	// or an empty string if the container doesn't exist.
	Status(ctx context.Context, name string) (string, error)
	// Inspect returns detailed container information.
	Inspect(ctx context.Context, name string) (*ContainerInfo, error)
	// Logs streams container logs to stdout/stderr. If follow is true, it
	// streams continuously until interrupted. If tail > 0, only the last N
	// lines are shown.
	Logs(ctx context.Context, name string, follow bool, tail int) error
	// Images lists locally cached container images matching the given reference
	// filter pattern (e.g. "*klaus-*"). If filter is empty, all images are returned.
	Images(ctx context.Context, filter string) ([]ImageInfo, error)
}

// RunOptions configures a container run invocation.
type RunOptions struct {
	// Name is the container name.
	Name string
	// Image is the container image reference.
	Image string
	// Detach runs the container in background.
	Detach bool
	// User overrides the container user (e.g. "1000:1000").
	// This is essential for bind mounts so the container process matches
	// the host UID that owns the mounted files.
	User string
	// EnvVars are environment variables to set.
	EnvVars map[string]string
	// Volumes are bind mount specifications.
	Volumes []Volume
	// Ports maps host ports to container ports.
	Ports map[int]int
}

// Volume represents a bind mount.
type Volume struct {
	// HostPath is the path on the host.
	HostPath string
	// ContainerPath is the path inside the container.
	ContainerPath string
	// ReadOnly marks the mount as read-only.
	ReadOnly bool
}

// ImageInfo holds information about a locally cached container image.
type ImageInfo struct {
	// Repository is the full image repository name (e.g. "gsoci.azurecr.io/giantswarm/klaus-go").
	Repository string
	// Tag is the image tag (e.g. "1.0.0").
	Tag string
	// ID is the image ID (short hash).
	ID string
	// CreatedSince is a human-readable relative time (e.g. "2 hours ago").
	CreatedSince string
	// Size is a human-readable image size (e.g. "500MB").
	Size string
}

// ContainerInfo holds information about a running container.
type ContainerInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Image     string    `json:"image"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"startedAt"`
}

// New creates a runtime for the given name ("docker" or "podman").
// If name is empty, it auto-detects the available runtime.
func New(name string) (Runtime, error) {
	if name == "" {
		detected, err := Detect()
		if err != nil {
			return nil, err
		}
		name = detected
	}

	switch name {
	case "docker", "podman":
		return &execRuntime{binary: name}, nil
	default:
		return nil, fmt.Errorf("unsupported runtime %q; use 'docker' or 'podman'", name)
	}
}

// inspectResult is the JSON structure returned by docker/podman inspect.
type inspectResult struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	Image string `json:"Image"`
	State struct {
		Status    string    `json:"Status"`
		Running   bool      `json:"Running"`
		StartedAt time.Time `json:"StartedAt"`
	} `json:"State"`
}
