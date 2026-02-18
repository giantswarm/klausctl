// Package instance tracks the state of running klausctl container instances.
package instance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/giantswarm/klausctl/pkg/config"
)

// Instance holds the state of a running klausctl container.
type Instance struct {
	// Name is the instance name (default: "default").
	Name string `json:"name"`
	// ContainerID is the container ID returned by the runtime.
	ContainerID string `json:"containerID"`
	// Runtime is the container runtime used ("docker" or "podman").
	Runtime string `json:"runtime"`
	// Personality is the OCI reference of the resolved personality (empty when none).
	Personality string `json:"personality,omitempty"`
	// Image is the container image reference.
	Image string `json:"image"`
	// Port is the host port mapped to the MCP endpoint.
	Port int `json:"port"`
	// Workspace is the host workspace directory.
	Workspace string `json:"workspace"`
	// StartedAt is when the container was started.
	StartedAt time.Time `json:"startedAt"`
}

// containerPrefix is the prefix used for all klausctl container names.
const containerPrefix = "klausctl-"

// ContainerName returns the container name for a given instance name.
func ContainerName(name string) string {
	return containerPrefix + name
}

// ContainerName returns the container name used by the runtime.
func (i *Instance) ContainerName() string {
	return ContainerName(i.Name)
}

// Save writes the instance state to the instance file.
// The caller is responsible for setting StartedAt before calling Save.
func (i *Instance) Save(paths *config.Paths) error {
	if err := config.EnsureDir(paths.InstanceDir); err != nil {
		return fmt.Errorf("creating instance directory: %w", err)
	}

	data, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling instance: %w", err)
	}

	return os.WriteFile(paths.InstanceFile, append(data, '\n'), 0o644)
}

// LoadAll reads instance state files from all per-instance directories.
func LoadAll(paths *config.Paths) ([]*Instance, error) {
	entries, err := os.ReadDir(paths.InstancesDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading instances directory: %w", err)
	}

	instances := make([]*Instance, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		instanceFile := filepath.Join(paths.InstancesDir, entry.Name(), "instance.json")
		data, err := os.ReadFile(instanceFile)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("reading instance state %s: %w", entry.Name(), err)
		}

		inst := &Instance{}
		if err := json.Unmarshal(data, inst); err != nil {
			return nil, fmt.Errorf("parsing instance state %s: %w", entry.Name(), err)
		}

		if inst.Name == "" {
			inst.Name = entry.Name()
		}
		instances = append(instances, inst)
	}

	return instances, nil
}

// Load reads the instance state from the instance file.
func Load(paths *config.Paths) (*Instance, error) {
	data, err := os.ReadFile(paths.InstanceFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no instance found; run 'klausctl start' first")
		}
		return nil, fmt.Errorf("reading instance state: %w", err)
	}

	inst := &Instance{}
	if err := json.Unmarshal(data, inst); err != nil {
		return nil, fmt.Errorf("parsing instance state: %w", err)
	}

	return inst, nil
}

// Clear removes the instance state file.
func Clear(paths *config.Paths) error {
	err := os.Remove(paths.InstanceFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
