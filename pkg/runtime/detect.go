package runtime

import (
	"fmt"
	"os/exec"
)

// Detect returns the name of the first available container runtime.
// It prefers docker over podman when both are available, since docker
// requires less configuration for bind mounts and UID mapping.
func Detect() (string, error) {
	for _, name := range []string{"docker", "podman"} {
		if _, err := exec.LookPath(name); err == nil {
			return name, nil
		}
	}
	return "", fmt.Errorf("no container runtime found; install docker or podman")
}
