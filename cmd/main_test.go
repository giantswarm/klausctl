package cmd

import (
	"errors"
	"os"
	"testing"

	runtimepkg "github.com/giantswarm/klausctl/pkg/runtime"
)

// errNoRuntimeInTests is what newRuntime returns unless a test installs a
// fake via overrideRuntime. It keeps the "no container runtime" failure path
// deterministic and guarantees that `go test` never starts, stops, or removes
// a real container, whether or not docker/podman is installed on the host.
var errNoRuntimeInTests = errors.New("no container runtime in tests; call overrideRuntime(t, fake) to inject one")

func TestMain(m *testing.M) {
	newRuntime = func(string) (runtimepkg.Runtime, error) { return nil, errNoRuntimeInTests }
	os.Exit(m.Run())
}
