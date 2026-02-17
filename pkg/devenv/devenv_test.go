package devenv

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

func TestGenerateDockerfile(t *testing.T) {
	t.Run("without packages", func(t *testing.T) {
		df := GenerateDockerfile("klaus:latest", "golang:1.25", nil)

		if !strings.Contains(df, "FROM klaus:latest AS klaus-source") {
			t.Error("should contain FROM with klaus image as build stage")
		}
		if !strings.Contains(df, "FROM golang:1.25") {
			t.Error("should contain FROM with base image")
		}
		if !strings.Contains(df, "ca-certificates curl git openssh-client") {
			t.Error("should install system dependencies")
		}
		if !strings.Contains(df, "COPY --from=klaus-source /usr/local/bin/klaus /usr/local/bin/klaus") {
			t.Error("should copy klaus binary from source stage")
		}
		if !strings.Contains(df, "COPY --from=klaus-source /usr/local/bin/claude /usr/local/bin/claude") {
			t.Error("should copy claude CLI from source stage")
		}
		if !strings.Contains(df, "COPY --from=klaus-source /usr/local/lib/node_modules/@anthropic-ai") {
			t.Error("should copy anthropic node modules from source stage")
		}
		if !strings.Contains(df, `ENTRYPOINT ["klaus"]`) {
			t.Error("should set klaus as entrypoint")
		}
		if !strings.Contains(df, "WORKDIR /workspace") {
			t.Error("should set /workspace as working directory")
		}
		if !strings.Contains(df, "EXPOSE 8080") {
			t.Error("should expose port 8080")
		}
		if strings.Contains(df, "# Additional packages") {
			t.Error("should not contain additional packages section")
		}
	})

	t.Run("with packages", func(t *testing.T) {
		df := GenerateDockerfile("klaus:v1", "python:3.12", []string{"make", "gcc"})

		if !strings.Contains(df, "FROM klaus:v1 AS klaus-source") {
			t.Error("should contain FROM with klaus image")
		}
		if !strings.Contains(df, "FROM python:3.12") {
			t.Error("should contain FROM with base image")
		}
		if !strings.Contains(df, "# Additional packages") {
			t.Error("should contain additional packages section")
		}
		if !strings.Contains(df, "make gcc") {
			t.Error("should contain the requested packages")
		}
	})

	t.Run("empty packages slice", func(t *testing.T) {
		df := GenerateDockerfile("klaus:latest", "golang:1.25", []string{})

		if strings.Contains(df, "# Additional packages") {
			t.Error("should not contain additional packages section for empty slice")
		}
	})

	t.Run("node.js conditional install", func(t *testing.T) {
		df := GenerateDockerfile("klaus:latest", "golang:1.25", nil)

		if !strings.Contains(df, "command -v node") {
			t.Error("should check for existing node.js installation")
		}
		if !strings.Contains(df, "nodesource.com") {
			t.Error("should install node.js from nodesource")
		}
	})
}

func TestCompositeTag(t *testing.T) {
	t.Run("deterministic", func(t *testing.T) {
		tag1 := CompositeTag("klaus:v1", "golang:1.25", []string{"make"})
		tag2 := CompositeTag("klaus:v1", "golang:1.25", []string{"make"})
		if tag1 != tag2 {
			t.Errorf("tags should be deterministic: %s != %s", tag1, tag2)
		}
	})

	t.Run("format", func(t *testing.T) {
		tag := CompositeTag("klaus:v1", "golang:1.25", nil)
		if !strings.HasPrefix(tag, "klausctl-toolchain:") {
			t.Errorf("tag should start with klausctl-toolchain:, got %s", tag)
		}
		// Tag should be "klausctl-toolchain:" + 24 hex chars (12 bytes).
		parts := strings.SplitN(tag, ":", 2)
		if len(parts[1]) != 24 {
			t.Errorf("hash part should be 24 hex chars, got %d: %s", len(parts[1]), parts[1])
		}
	})

	t.Run("different inputs produce different tags", func(t *testing.T) {
		tags := []string{
			CompositeTag("klaus:v1", "golang:1.25", nil),
			CompositeTag("klaus:v2", "golang:1.25", nil),
			CompositeTag("klaus:v1", "python:3.12", nil),
			CompositeTag("klaus:v1", "golang:1.25", []string{"make"}),
		}

		seen := make(map[string]bool)
		for _, tag := range tags {
			if seen[tag] {
				t.Errorf("duplicate tag: %s", tag)
			}
			seen[tag] = true
		}
	})

	t.Run("package order independent", func(t *testing.T) {
		tag1 := CompositeTag("klaus:v1", "golang:1.25", []string{"gcc", "make", "cmake"})
		tag2 := CompositeTag("klaus:v1", "golang:1.25", []string{"make", "cmake", "gcc"})
		if tag1 != tag2 {
			t.Errorf("package order should not affect tag: %s != %s", tag1, tag2)
		}
	})

	t.Run("nil and empty packages are equivalent", func(t *testing.T) {
		tag1 := CompositeTag("klaus:v1", "golang:1.25", nil)
		tag2 := CompositeTag("klaus:v1", "golang:1.25", []string{})
		if tag1 != tag2 {
			t.Errorf("nil and empty packages should produce same tag: %s != %s", tag1, tag2)
		}
	})
}

// mockRuntime implements runtime.Runtime for testing Build.
type mockRuntime struct {
	name           string
	imageExists    bool
	imageExistsErr error
	buildCalled    bool
	buildOpts      runtime.BuildOptions
	buildErr       error
}

// Compile-time interface check.
var _ runtime.Runtime = (*mockRuntime)(nil)

func (m *mockRuntime) Name() string { return m.name }

func (m *mockRuntime) ImageExists(_ context.Context, _ string) (bool, error) {
	return m.imageExists, m.imageExistsErr
}

func (m *mockRuntime) BuildImage(_ context.Context, opts runtime.BuildOptions) (string, error) {
	m.buildCalled = true
	m.buildOpts = opts
	if m.buildErr != nil {
		return "", m.buildErr
	}
	return opts.Tag, nil
}

func (m *mockRuntime) Run(context.Context, runtime.RunOptions) (string, error) {
	return "", fmt.Errorf("unexpected call to Run")
}

func (m *mockRuntime) Stop(context.Context, string) error {
	return fmt.Errorf("unexpected call to Stop")
}

func (m *mockRuntime) Remove(context.Context, string) error {
	return fmt.Errorf("unexpected call to Remove")
}

func (m *mockRuntime) Status(context.Context, string) (string, error) {
	return "", fmt.Errorf("unexpected call to Status")
}

func (m *mockRuntime) Inspect(context.Context, string) (*runtime.ContainerInfo, error) {
	return nil, fmt.Errorf("unexpected call to Inspect")
}

func (m *mockRuntime) Logs(context.Context, string, bool, int) error {
	return fmt.Errorf("unexpected call to Logs")
}

func TestBuild(t *testing.T) {
	t.Run("skips build when image exists", func(t *testing.T) {
		dir := t.TempDir()
		rt := &mockRuntime{name: "docker", imageExists: true}
		tc := &config.Toolchain{Image: "golang:1.25"}

		tag, err := Build(context.Background(), rt, "klaus:v1", tc, dir, io.Discard)
		if err != nil {
			t.Fatalf("Build() returned error: %v", err)
		}
		if tag == "" {
			t.Error("Build() should return a non-empty tag")
		}
		if rt.buildCalled {
			t.Error("Build() should not call BuildImage when image exists")
		}

		// Dockerfile should still be written for debugging.
		dfPath := filepath.Join(dir, "Dockerfile.toolchain")
		if _, err := os.Stat(dfPath); err != nil {
			t.Error("Dockerfile.toolchain should be written even when image exists")
		}
	})

	t.Run("builds when image does not exist", func(t *testing.T) {
		dir := t.TempDir()
		rt := &mockRuntime{name: "docker", imageExists: false}
		tc := &config.Toolchain{
			Image:    "golang:1.25",
			Packages: []string{"make"},
		}

		tag, err := Build(context.Background(), rt, "klaus:v1", tc, dir, io.Discard)
		if err != nil {
			t.Fatalf("Build() returned error: %v", err)
		}
		if tag == "" {
			t.Error("Build() should return a non-empty tag")
		}
		if !rt.buildCalled {
			t.Error("Build() should call BuildImage when image does not exist")
		}
		if rt.buildOpts.Tag != tag {
			t.Errorf("BuildImage tag = %s, want %s", rt.buildOpts.Tag, tag)
		}
		if rt.buildOpts.Context != dir {
			t.Errorf("BuildImage context = %s, want %s", rt.buildOpts.Context, dir)
		}

		// Verify the Dockerfile path is correctly set.
		expectedDF := filepath.Join(dir, "Dockerfile.toolchain")
		if rt.buildOpts.Dockerfile != expectedDF {
			t.Errorf("BuildImage Dockerfile = %s, want %s", rt.buildOpts.Dockerfile, expectedDF)
		}
	})

	t.Run("returns deterministic tag", func(t *testing.T) {
		dir1 := t.TempDir()
		dir2 := t.TempDir()
		tc := &config.Toolchain{Image: "golang:1.25"}

		rt1 := &mockRuntime{name: "docker", imageExists: true}
		tag1, err := Build(context.Background(), rt1, "klaus:v1", tc, dir1, io.Discard)
		if err != nil {
			t.Fatalf("first Build() returned error: %v", err)
		}

		rt2 := &mockRuntime{name: "docker", imageExists: true}
		tag2, err := Build(context.Background(), rt2, "klaus:v1", tc, dir2, io.Discard)
		if err != nil {
			t.Fatalf("second Build() returned error: %v", err)
		}

		if tag1 != tag2 {
			t.Errorf("Build() should return deterministic tags: %s != %s", tag1, tag2)
		}
	})

	t.Run("writes correct Dockerfile content", func(t *testing.T) {
		dir := t.TempDir()
		rt := &mockRuntime{name: "docker", imageExists: true}
		tc := &config.Toolchain{
			Image:    "golang:1.25",
			Packages: []string{"make", "gcc"},
		}

		_, err := Build(context.Background(), rt, "klaus:v1", tc, dir, io.Discard)
		if err != nil {
			t.Fatalf("Build() returned error: %v", err)
		}

		dfPath := filepath.Join(dir, "Dockerfile.toolchain")
		data, err := os.ReadFile(dfPath)
		if err != nil {
			t.Fatalf("reading Dockerfile: %v", err)
		}

		content := string(data)
		if !strings.Contains(content, "FROM klaus:v1 AS klaus-source") {
			t.Error("Dockerfile should reference the klaus image")
		}
		if !strings.Contains(content, "FROM golang:1.25") {
			t.Error("Dockerfile should reference the base image")
		}
		if !strings.Contains(content, "make gcc") {
			t.Error("Dockerfile should contain requested packages")
		}
	})

	t.Run("returns error on build failure", func(t *testing.T) {
		dir := t.TempDir()
		rt := &mockRuntime{
			name:        "docker",
			imageExists: false,
			buildErr:    fmt.Errorf("build failed"),
		}
		tc := &config.Toolchain{Image: "golang:1.25"}

		_, err := Build(context.Background(), rt, "klaus:v1", tc, dir, io.Discard)
		if err == nil {
			t.Fatal("Build() should return error on build failure")
		}
		if !strings.Contains(err.Error(), "building toolchain image") {
			t.Errorf("error should wrap build failure: %v", err)
		}
	})

	t.Run("returns error on image exists check failure", func(t *testing.T) {
		dir := t.TempDir()
		rt := &mockRuntime{
			name:           "docker",
			imageExistsErr: fmt.Errorf("connection refused"),
		}
		tc := &config.Toolchain{Image: "golang:1.25"}

		_, err := Build(context.Background(), rt, "klaus:v1", tc, dir, io.Discard)
		if err == nil {
			t.Fatal("Build() should return error on ImageExists failure")
		}
		if !strings.Contains(err.Error(), "checking for existing image") {
			t.Errorf("error should wrap ImageExists failure: %v", err)
		}
	})
}
