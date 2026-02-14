package runtime

import (
	"fmt"
	"os/exec"
)

// Detect returns the name of the first available container runtime.
// It checks for docker first, then podman.
func Detect() (string, error) {
	for _, name := range []string{"docker", "podman"} {
		if _, err := exec.LookPath(name); err == nil {
			return name, nil
		}
	}
	return "", fmt.Errorf("no container runtime found; install docker or podman")
}
