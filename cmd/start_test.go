package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

// startMockRuntime implements runtime.Runtime for testing resolveImage.
type startMockRuntime struct {
	name           string
	imageExists    bool
	imageExistsErr error
	buildCalled    bool
	buildOpts      runtime.BuildOptions
	buildErr       error
}

var _ runtime.Runtime = (*startMockRuntime)(nil)

func (m *startMockRuntime) Name() string { return m.name }

func (m *startMockRuntime) ImageExists(_ context.Context, _ string) (bool, error) {
	return m.imageExists, m.imageExistsErr
}

func (m *startMockRuntime) BuildImage(_ context.Context, opts runtime.BuildOptions) (string, error) {
	m.buildCalled = true
	m.buildOpts = opts
	if m.buildErr != nil {
		return "", m.buildErr
	}
	return opts.Tag, nil
}

func (m *startMockRuntime) Run(context.Context, runtime.RunOptions) (string, error) {
	return "", fmt.Errorf("unexpected call to Run")
}

func (m *startMockRuntime) Stop(context.Context, string) error {
	return fmt.Errorf("unexpected call to Stop")
}

func (m *startMockRuntime) Remove(context.Context, string) error {
	return fmt.Errorf("unexpected call to Remove")
}

func (m *startMockRuntime) Status(context.Context, string) (string, error) {
	return "", fmt.Errorf("unexpected call to Status")
}

func (m *startMockRuntime) Inspect(context.Context, string) (*runtime.ContainerInfo, error) {
	return nil, fmt.Errorf("unexpected call to Inspect")
}

func (m *startMockRuntime) Logs(context.Context, string, bool, int) error {
	return fmt.Errorf("unexpected call to Logs")
}

func TestResolveImage(t *testing.T) {
	t.Run("returns default image when no toolchain", func(t *testing.T) {
		cfg := &config.Config{
			Image: "gsoci.azurecr.io/giantswarm/klaus:latest",
		}

		image, err := resolveImage(context.Background(), cfg, nil, "", io.Discard)
		if err != nil {
			t.Fatalf("resolveImage() error: %v", err)
		}
		if image != "gsoci.azurecr.io/giantswarm/klaus:latest" {
			t.Errorf("image = %q, want %q", image, "gsoci.azurecr.io/giantswarm/klaus:latest")
		}
	})

	t.Run("returns prebuilt toolchain image", func(t *testing.T) {
		cfg := &config.Config{
			Image: "gsoci.azurecr.io/giantswarm/klaus:latest",
			Toolchain: &config.Toolchain{
				Image:    "gsoci.azurecr.io/giantswarm/klaus-go:1.0.0",
				Prebuilt: true,
			},
		}

		image, err := resolveImage(context.Background(), cfg, nil, "", io.Discard)
		if err != nil {
			t.Fatalf("resolveImage() error: %v", err)
		}
		if image != "gsoci.azurecr.io/giantswarm/klaus-go:1.0.0" {
			t.Errorf("image = %q, want %q", image, "gsoci.azurecr.io/giantswarm/klaus-go:1.0.0")
		}
	})

	t.Run("prebuilt toolchain ignores default image", func(t *testing.T) {
		cfg := &config.Config{
			Image: "gsoci.azurecr.io/giantswarm/klaus:v2",
			Toolchain: &config.Toolchain{
				Image:    "gsoci.azurecr.io/giantswarm/klaus-python:1.0.0",
				Prebuilt: true,
			},
		}

		image, err := resolveImage(context.Background(), cfg, nil, "", io.Discard)
		if err != nil {
			t.Fatalf("resolveImage() error: %v", err)
		}
		if image != "gsoci.azurecr.io/giantswarm/klaus-python:1.0.0" {
			t.Errorf("image = %q, want %q", image, "gsoci.azurecr.io/giantswarm/klaus-python:1.0.0")
		}
	})

	t.Run("builds composite image for non-prebuilt toolchain", func(t *testing.T) {
		dir := t.TempDir()
		rt := &startMockRuntime{name: "docker", imageExists: false}
		cfg := &config.Config{
			Image: "gsoci.azurecr.io/giantswarm/klaus:latest",
			Toolchain: &config.Toolchain{
				Image: "golang:1.25",
			},
		}

		image, err := resolveImage(context.Background(), cfg, rt, dir, io.Discard)
		if err != nil {
			t.Fatalf("resolveImage() error: %v", err)
		}
		if image == "" {
			t.Fatal("resolveImage() returned empty image")
		}
		if !strings.HasPrefix(image, "klausctl-toolchain:") {
			t.Errorf("expected composite tag prefix, got %q", image)
		}
		if !rt.buildCalled {
			t.Error("expected BuildImage to be called for non-prebuilt toolchain")
		}
	})

	t.Run("skips build when composite image exists locally", func(t *testing.T) {
		dir := t.TempDir()
		rt := &startMockRuntime{name: "docker", imageExists: true}
		cfg := &config.Config{
			Image: "gsoci.azurecr.io/giantswarm/klaus:latest",
			Toolchain: &config.Toolchain{
				Image: "golang:1.25",
			},
		}

		image, err := resolveImage(context.Background(), cfg, rt, dir, io.Discard)
		if err != nil {
			t.Fatalf("resolveImage() error: %v", err)
		}
		if !strings.HasPrefix(image, "klausctl-toolchain:") {
			t.Errorf("expected composite tag prefix, got %q", image)
		}
		if rt.buildCalled {
			t.Error("BuildImage should not be called when image exists locally")
		}
	})

	t.Run("returns error on composite build failure", func(t *testing.T) {
		dir := t.TempDir()
		rt := &startMockRuntime{
			name:        "docker",
			imageExists: false,
			buildErr:    fmt.Errorf("build failed"),
		}
		cfg := &config.Config{
			Image: "gsoci.azurecr.io/giantswarm/klaus:latest",
			Toolchain: &config.Toolchain{
				Image: "golang:1.25",
			},
		}

		_, err := resolveImage(context.Background(), cfg, rt, dir, io.Discard)
		if err == nil {
			t.Fatal("resolveImage() should return error on build failure")
		}
		if !strings.Contains(err.Error(), "building toolchain image") {
			t.Errorf("error should wrap build failure: %v", err)
		}
	})

	t.Run("composite build includes extra packages", func(t *testing.T) {
		dir := t.TempDir()
		rt := &startMockRuntime{name: "docker", imageExists: false}
		cfg := &config.Config{
			Image: "gsoci.azurecr.io/giantswarm/klaus:latest",
			Toolchain: &config.Toolchain{
				Image:    "golang:1.25",
				Packages: []string{"make", "gcc"},
			},
		}

		image, err := resolveImage(context.Background(), cfg, rt, dir, io.Discard)
		if err != nil {
			t.Fatalf("resolveImage() error: %v", err)
		}
		if !strings.HasPrefix(image, "klausctl-toolchain:") {
			t.Errorf("expected composite tag prefix, got %q", image)
		}

		// Verify a different tag is produced vs without packages.
		cfgNoPackages := &config.Config{
			Image: "gsoci.azurecr.io/giantswarm/klaus:latest",
			Toolchain: &config.Toolchain{
				Image: "golang:1.25",
			},
		}
		rt2 := &startMockRuntime{name: "docker", imageExists: false}
		imageNoPackages, err := resolveImage(context.Background(), cfgNoPackages, rt2, t.TempDir(), io.Discard)
		if err != nil {
			t.Fatalf("resolveImage() error: %v", err)
		}

		if image == imageNoPackages {
			t.Error("toolchain with packages should produce a different tag than without")
		}
	})
}

func TestStartSubcommandRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "start" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'start' subcommand to be registered on rootCmd")
	}
}

func TestStartWorkspaceFlag(t *testing.T) {
	f := startCmd.Flags().Lookup("workspace")
	if f == nil {
		t.Fatal("expected --workspace flag to be registered")
	}
}
