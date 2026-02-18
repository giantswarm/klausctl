package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/runtime"
)

// mockRuntime implements runtime.Runtime for testing.
type mockRuntime struct {
	images []runtime.ImageInfo
	err    error
}

func (m *mockRuntime) Name() string                                                { return "mock" }
func (m *mockRuntime) Run(_ context.Context, _ runtime.RunOptions) (string, error) { return "", nil }
func (m *mockRuntime) Stop(_ context.Context, _ string) error                      { return nil }
func (m *mockRuntime) Remove(_ context.Context, _ string) error                    { return nil }
func (m *mockRuntime) Status(_ context.Context, _ string) (string, error)          { return "", nil }
func (m *mockRuntime) Inspect(_ context.Context, _ string) (*runtime.ContainerInfo, error) {
	return nil, nil
}
func (m *mockRuntime) Pull(_ context.Context, _ string, _ io.Writer) error   { return nil }
func (m *mockRuntime) Logs(_ context.Context, _ string, _ bool, _ int) error { return nil }
func (m *mockRuntime) Images(_ context.Context, _ string) ([]runtime.ImageInfo, error) {
	return m.images, m.err
}

func TestSubcommandsRegistered(t *testing.T) {
	tests := []struct {
		name   string
		parent *cobra.Command
		sub    string
	}{
		{"toolchain on root", rootCmd, "toolchain"},
		{"completion on root", rootCmd, "completion"},
		{"plugin on root", rootCmd, "plugin"},
		{"personality on root", rootCmd, "personality"},
		{"list on toolchain", toolchainCmd, "list"},
		{"init on toolchain", toolchainCmd, "init"},
		{"validate on toolchain", toolchainCmd, "validate"},
		{"pull on toolchain", toolchainCmd, "pull"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, cmd := range tt.parent.Commands() {
				if cmd.Name() == tt.sub {
					return
				}
			}
			t.Errorf("expected %q subcommand to be registered", tt.sub)
		})
	}
}

func TestToolchainInitNameFlagRequired(t *testing.T) {
	f := toolchainInitCmd.Flags().Lookup("name")
	if f == nil {
		t.Fatal("expected --name flag to be registered")
	}
}

func TestToolchainInitDirFlag(t *testing.T) {
	f := toolchainInitCmd.Flags().Lookup("dir")
	if f == nil {
		t.Fatal("expected --dir flag to be registered")
	}
}

func TestToolchainListRemoteFlag(t *testing.T) {
	f := toolchainListCmd.Flags().Lookup("remote")
	if f == nil {
		t.Fatal("expected --remote flag to be registered")
	}
}

func TestScaffoldFiles(t *testing.T) {
	files := scaffoldFiles("go")

	expectedFiles := []string{
		"Dockerfile",
		"Dockerfile.debian",
		"Makefile",
		".circleci/config.yml",
		"README.md",
	}

	for _, name := range expectedFiles {
		content, ok := files[name]
		if !ok {
			t.Errorf("expected scaffold file %q to exist", name)
			continue
		}
		if content == "" {
			t.Errorf("expected scaffold file %q to have content", name)
		}
	}

	if len(files) != len(expectedFiles) {
		t.Errorf("expected %d scaffold files, got %d", len(expectedFiles), len(files))
	}
}

func TestScaffoldFilesContainToolchainName(t *testing.T) {
	files := scaffoldFiles("python")

	if !strings.Contains(files["Dockerfile"], "klaus-python") {
		t.Error("Dockerfile should reference the toolchain name")
	}
	if !strings.Contains(files["Dockerfile.debian"], "klaus-python") {
		t.Error("Dockerfile.debian should reference the toolchain name")
	}
	if !strings.Contains(files["Makefile"], "klaus-python") {
		t.Error("Makefile should reference the toolchain name")
	}
	if !strings.Contains(files["README.md"], "klaus-python") {
		t.Error("README.md should reference the toolchain name")
	}
}

func TestScaffoldFilesImageName(t *testing.T) {
	files := scaffoldFiles("go")

	expectedImage := "gsoci.azurecr.io/giantswarm/klaus-go"
	if !strings.Contains(files["Makefile"], expectedImage) {
		t.Errorf("Makefile should contain image name %q", expectedImage)
	}
	if !strings.Contains(files["README.md"], expectedImage) {
		t.Errorf("README.md should contain image name %q", expectedImage)
	}
}

func TestRunToolchainInit(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "klaus-test-toolchain")

	toolchainInitName = "test-toolchain"
	toolchainInitDir = outDir

	var buf bytes.Buffer
	toolchainInitCmd.SetOut(&buf)

	err := runToolchainInit(toolchainInitCmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Created") {
		t.Error("expected output to contain 'Created'")
	}

	expectedFiles := []string{
		"Dockerfile",
		"Dockerfile.debian",
		"Makefile",
		".circleci/config.yml",
		"README.md",
	}
	for _, name := range expectedFiles {
		path := filepath.Join(outDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %q to be created", name)
		}
	}
}

func TestRunToolchainInitExistingDir(t *testing.T) {
	dir := t.TempDir()

	toolchainInitName = "existing"
	toolchainInitDir = dir

	err := runToolchainInit(toolchainInitCmd, nil)
	if err == nil {
		t.Fatal("expected error when directory already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestPrintImageTable(t *testing.T) {
	var buf bytes.Buffer

	images := []runtime.ImageInfo{
		{
			Repository:   "gsoci.azurecr.io/giantswarm/klaus-go",
			Tag:          "1.0.0",
			CreatedSince: "2 hours ago",
		},
		{
			Repository:   "gsoci.azurecr.io/giantswarm/klaus-python",
			Tag:          "1.0.0",
			CreatedSince: "5 minutes ago",
		},
	}

	if err := printImageTable(&buf, images, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()

	if !strings.Contains(output, "IMAGE") {
		t.Error("expected table header to contain IMAGE")
	}
	if !strings.Contains(output, "TAG") {
		t.Error("expected table header to contain TAG")
	}
	if !strings.Contains(output, "CREATED") {
		t.Error("expected table header to contain CREATED")
	}

	if !strings.Contains(output, "klaus-go") {
		t.Error("expected output to contain 'klaus-go'")
	}
	if !strings.Contains(output, "klaus-python") {
		t.Error("expected output to contain 'klaus-python'")
	}
	if !strings.Contains(output, "1.0.0") {
		t.Error("expected output to contain '1.0.0'")
	}
}

func TestToolchainListWithImages(t *testing.T) {
	rt := &mockRuntime{
		images: []runtime.ImageInfo{
			{Repository: "gsoci.azurecr.io/giantswarm/klaus-go", Tag: "1.0.0", CreatedSince: "2 hours ago"},
			{Repository: "gsoci.azurecr.io/giantswarm/klaus-python", Tag: "2.1.0", CreatedSince: "1 day ago"},
			{Repository: "gsoci.azurecr.io/giantswarm/klaus", Tag: "latest", CreatedSince: "3 days ago"},
			{Repository: "docker.io/library/alpine", Tag: "3.19", CreatedSince: "4 weeks ago"},
		},
	}

	var buf bytes.Buffer
	err := toolchainList(context.Background(), &buf, rt, toolchainListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "klaus-go") {
		t.Error("expected output to contain 'klaus-go'")
	}
	if !strings.Contains(output, "klaus-python") {
		t.Error("expected output to contain 'klaus-python'")
	}
	if !strings.Contains(output, "2.1.0") {
		t.Error("expected output to contain '2.1.0'")
	}
	if strings.Contains(output, "alpine") {
		t.Error("expected non-toolchain image 'alpine' to be filtered out")
	}
	if strings.Contains(output, "3 days ago") {
		t.Error("expected base 'klaus' image to be filtered out")
	}
}

func TestToolchainListEmpty(t *testing.T) {
	rt := &mockRuntime{}

	var buf bytes.Buffer
	err := toolchainList(context.Background(), &buf, rt, toolchainListOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No toolchain images found locally") {
		t.Error("expected empty-state message")
	}
}

func TestToolchainListError(t *testing.T) {
	rt := &mockRuntime{err: fmt.Errorf("connection refused")}

	var buf bytes.Buffer
	err := toolchainList(context.Background(), &buf, rt, toolchainListOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "listing images") {
		t.Errorf("expected wrapped error, got: %v", err)
	}
}

func TestToolchainListJSON(t *testing.T) {
	rt := &mockRuntime{
		images: []runtime.ImageInfo{
			{Repository: "gsoci.azurecr.io/giantswarm/klaus-go", Tag: "1.0.0", Size: "500MB"},
		},
	}

	var buf bytes.Buffer
	err := toolchainList(context.Background(), &buf, rt, toolchainListOptions{output: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"repository"`) {
		t.Error("expected JSON output to contain 'repository' key")
	}
	if !strings.Contains(output, "klaus-go") {
		t.Error("expected JSON output to contain 'klaus-go'")
	}
}

func TestToolchainListJSONEmpty(t *testing.T) {
	rt := &mockRuntime{}

	var buf bytes.Buffer
	err := toolchainList(context.Background(), &buf, rt, toolchainListOptions{output: "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := strings.TrimSpace(buf.String())
	if output != "[]" {
		t.Errorf("expected empty JSON array, got: %s", output)
	}
}

func TestToolchainListWide(t *testing.T) {
	rt := &mockRuntime{
		images: []runtime.ImageInfo{
			{Repository: "gsoci.azurecr.io/giantswarm/klaus-go", Tag: "1.0.0", ID: "abc123", Size: "500MB", CreatedSince: "2h ago"},
		},
	}

	var buf bytes.Buffer
	err := toolchainList(context.Background(), &buf, rt, toolchainListOptions{wide: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "ID") {
		t.Error("expected wide output to contain ID column")
	}
	if !strings.Contains(output, "abc123") {
		t.Error("expected wide output to contain image ID")
	}
	if !strings.Contains(output, "500MB") {
		t.Error("expected wide output to contain image size")
	}
}

func TestValidateToolchainDirValid(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := validateToolchainDir(dir)
	if err != nil {
		t.Errorf("validateToolchainDir() error = %v", err)
	}
}

func TestValidateToolchainDirMissingDockerfile(t *testing.T) {
	dir := t.TempDir()

	err := validateToolchainDir(dir)
	if err == nil {
		t.Fatal("expected error for missing Dockerfile")
	}
	if !strings.Contains(err.Error(), "Dockerfile not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateToolchainDirNotExist(t *testing.T) {
	err := validateToolchainDir("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("unexpected error: %v", err)
	}
}
