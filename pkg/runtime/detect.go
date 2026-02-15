package runtime

import (
	"fmt"
	"os/exec"
)

// Detect returns the name of the first available container runtime.
// It prefers podman over docker when both are available, consistent
// with the convention that rootless podman is the safer default.
func Detect() (string, error) {
	for _, name := range []string{"podman", "docker"} {
		if _, err := exec.LookPath(name); err == nil {
			return name, nil
		}
	}
	return "", fmt.Errorf("no container runtime found; install docker or podman")
}
