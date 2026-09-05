package instance

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"gopkg.in/yaml.v3"

	"github.com/giantswarm/klausctl/internal/server"
	runtimepkg "github.com/giantswarm/klausctl/pkg/runtime"
)

// errNoRuntimeInTests is what newRuntime returns unless a test installs a
// fake via useFakeRuntime. It keeps the "no container runtime" failure path
// deterministic and guarantees that `go test` never starts, stops, or removes
// a real container, whether or not docker/podman is installed on the host.
var errNoRuntimeInTests = errors.New("no container runtime in tests; call useFakeRuntime(t) to inject one")

func TestMain(m *testing.M) {
	newRuntime = func(string) (runtimepkg.Runtime, error) { return nil, errNoRuntimeInTests }
	os.Exit(m.Run())
}

// fakeRuntime is an in-memory runtime.Runtime. It records the container
// names passed to Run and never touches docker or podman.
type fakeRuntime struct {
	runCalls []string
}

func (f *fakeRuntime) Name() string { return "fake" }
func (f *fakeRuntime) Run(_ context.Context, opts runtimepkg.RunOptions) (string, error) {
	f.runCalls = append(f.runCalls, opts.Name)
	return "fake-container-id", nil
}
func (f *fakeRuntime) Stop(_ context.Context, _ string) error             { return nil }
func (f *fakeRuntime) Remove(_ context.Context, _ string) error           { return nil }
func (f *fakeRuntime) Status(_ context.Context, _ string) (string, error) { return "", nil }
func (f *fakeRuntime) Inspect(_ context.Context, _ string) (*runtimepkg.ContainerInfo, error) {
	return &runtimepkg.ContainerInfo{StartedAt: time.Now()}, nil
}
func (f *fakeRuntime) Logs(_ context.Context, _ string, _ bool, _ int) error { return nil }
func (f *fakeRuntime) LogsCapture(_ context.Context, _ string, _ int) (string, error) {
	return "", nil
}
func (f *fakeRuntime) Pull(_ context.Context, _ string, _ io.Writer) error { return nil }
func (f *fakeRuntime) Images(_ context.Context, _ string) ([]runtimepkg.ImageInfo, error) {
	return nil, nil
}

// useFakeRuntime makes create and start succeed against a fake runtime for
// the duration of the test and returns the fake for assertions.
func useFakeRuntime(t *testing.T) *fakeRuntime {
	t.Helper()
	rt := &fakeRuntime{}
	orig := newRuntime
	newRuntime = func(string) (runtimepkg.Runtime, error) { return rt, nil }
	t.Cleanup(func() { newRuntime = orig })
	return rt
}

// readInstanceConfig fails the test if the create result is an error and
// otherwise returns the named instance's config.yaml parsed into a map.
func readInstanceConfig(t *testing.T, sc *server.ServerContext, result *mcp.CallToolResult, name string) map[string]any {
	t.Helper()
	if result == nil || result.IsError {
		t.Fatalf("create failed: %s", extractResultText(t, result))
	}
	configPath := filepath.Join(sc.Paths.InstancesDir, name, "config.yaml")
	data, err := os.ReadFile(configPath) // #nosec G304 -- test-owned temp path
	if err != nil {
		t.Fatalf("reading instance config: %v", err)
	}
	var cfgMap map[string]any
	if err := yaml.Unmarshal(data, &cfgMap); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}
	return cfgMap
}

// freePort returns a TCP port that was free on 127.0.0.1 a moment ago, so a
// test that pins a port does not collide with whatever else the host runs.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}
