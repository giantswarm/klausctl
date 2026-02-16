package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeScript creates a small shell script in dir with the given name and body,
// and returns its path. The script is made executable.
func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBuildImageArgs(t *testing.T) {
	binDir := t.TempDir()

	// Create a fake docker binary that records its arguments.
	argsFile := filepath.Join(binDir, "args.txt")
	writeScript(t, binDir, "docker", fmt.Sprintf(`printf '%%s\n' "$@" > %s`, argsFile))

	rt := &execRuntime{binary: filepath.Join(binDir, "docker")}
	ctx := context.Background()

	_, err := rt.BuildImage(ctx, BuildOptions{
		Tag:        "test-image:latest",
		Dockerfile: "/tmp/Dockerfile",
		Context:    "/tmp/context",
	})
	if err != nil {
		t.Fatalf("BuildImage() returned error: %v", err)
	}

	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("reading args file: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(data)), "\n")

	expected := []string{"build", "-t", "test-image:latest", "-f", "/tmp/Dockerfile", "/tmp/context"}
	if len(args) != len(expected) {
		t.Fatalf("args = %v, want %v", args, expected)
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("args[%d] = %q, want %q", i, a, expected[i])
		}
	}
}

func TestBuildImageWithoutDockerfile(t *testing.T) {
	binDir := t.TempDir()

	argsFile := filepath.Join(binDir, "args.txt")
	writeScript(t, binDir, "docker", fmt.Sprintf(`printf '%%s\n' "$@" > %s`, argsFile))

	rt := &execRuntime{binary: filepath.Join(binDir, "docker")}
	ctx := context.Background()

	_, err := rt.BuildImage(ctx, BuildOptions{
		Tag:     "test-image:latest",
		Context: "/tmp/context",
	})
	if err != nil {
		t.Fatalf("BuildImage() returned error: %v", err)
	}

	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("reading args file: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(data)), "\n")

	// Without Dockerfile, no -f flag should be present.
	expected := []string{"build", "-t", "test-image:latest", "/tmp/context"}
	if len(args) != len(expected) {
		t.Fatalf("args = %v, want %v", args, expected)
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("args[%d] = %q, want %q", i, a, expected[i])
		}
	}
}

func TestImageExistsReturnsTrue(t *testing.T) {
	binDir := t.TempDir()

	// Fake docker that exits 0 for image inspect.
	writeScript(t, binDir, "docker", `exit 0`)

	rt := &execRuntime{binary: filepath.Join(binDir, "docker")}
	ctx := context.Background()

	exists, err := rt.ImageExists(ctx, "test-image:latest")
	if err != nil {
		t.Fatalf("ImageExists() returned error: %v", err)
	}
	if !exists {
		t.Error("ImageExists() = false, want true")
	}
}

func TestImageExistsReturnsFalseForDocker(t *testing.T) {
	binDir := t.TempDir()

	// Fake docker that mimics "no such image" error.
	writeScript(t, binDir, "docker", `echo "Error: No such image: test-image:latest" >&2; exit 1`)

	rt := &execRuntime{binary: filepath.Join(binDir, "docker")}
	ctx := context.Background()

	exists, err := rt.ImageExists(ctx, "test-image:latest")
	if err != nil {
		t.Fatalf("ImageExists() returned error: %v", err)
	}
	if exists {
		t.Error("ImageExists() = true, want false")
	}
}

func TestImageExistsReturnsFalseForPodman(t *testing.T) {
	binDir := t.TempDir()

	// Fake podman that mimics "image not known" error.
	writeScript(t, binDir, "podman", `echo "Error: test-image:latest: image not known" >&2; exit 125`)

	rt := &execRuntime{binary: filepath.Join(binDir, "podman")}
	ctx := context.Background()

	exists, err := rt.ImageExists(ctx, "test-image:latest")
	if err != nil {
		t.Fatalf("ImageExists() returned error: %v", err)
	}
	if exists {
		t.Error("ImageExists() = true, want false")
	}
}

func TestImageExistsReturnsErrorOnUnexpectedFailure(t *testing.T) {
	binDir := t.TempDir()

	// Fake docker that fails with an unexpected error.
	writeScript(t, binDir, "docker", `echo "Error: connection refused" >&2; exit 1`)

	rt := &execRuntime{binary: filepath.Join(binDir, "docker")}
	ctx := context.Background()

	_, err := rt.ImageExists(ctx, "test-image:latest")
	if err == nil {
		t.Fatal("ImageExists() should return error for unexpected failure")
	}
	if !strings.Contains(err.Error(), "image inspect failed") {
		t.Errorf("error = %q, want substring %q", err.Error(), "image inspect failed")
	}
}

func TestBuildImageValidation(t *testing.T) {
	rt := &execRuntime{binary: "docker"}
	ctx := context.Background()

	tests := []struct {
		name   string
		opts   BuildOptions
		errMsg string
	}{
		{
			name:   "missing tag",
			opts:   BuildOptions{Context: "/tmp/context"},
			errMsg: "build tag is required",
		},
		{
			name:   "missing context",
			opts:   BuildOptions{Tag: "test:latest"},
			errMsg: "build context is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := rt.BuildImage(ctx, tt.opts)
			if err == nil {
				t.Fatal("BuildImage() should return error")
			}
			if !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.errMsg)
			}
		})
	}
}
