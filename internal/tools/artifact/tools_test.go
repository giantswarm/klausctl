package artifact

import (
	"os"
	"path/filepath"
	"testing"

	klausoci "github.com/giantswarm/klaus-oci"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/config"
)

func testServerContext(t *testing.T) *server.ServerContext {
	t.Helper()
	configHome := filepath.Join(t.TempDir(), "config-home")
	t.Setenv("XDG_CONFIG_HOME", configHome)

	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	return &server.ServerContext{Paths: paths}
}

func TestRegisterTools(t *testing.T) {
	sc := testServerContext(t)
	srv := mcpserver.NewMCPServer("test", "1.0.0",
		mcpserver.WithToolCapabilities(false),
	)
	RegisterTools(srv, sc)
}

func TestLatestSemverTag(t *testing.T) {
	tests := []struct {
		name string
		tags []string
		want string
	}{
		{
			name: "single tag",
			tags: []string{"1.0.0"},
			want: "1.0.0",
		},
		{
			name: "multiple versions",
			tags: []string{"1.0.0", "2.1.0", "1.5.3"},
			want: "2.1.0",
		},
		{
			name: "with non-semver tags",
			tags: []string{"latest", "1.0.0", "dev", "2.0.0", "nightly"},
			want: "2.0.0",
		},
		{
			name: "all non-semver",
			tags: []string{"latest", "dev", "nightly"},
			want: "",
		},
		{
			name: "empty list",
			tags: nil,
			want: "",
		},
		{
			name: "pre-release versions",
			tags: []string{"1.0.0-alpha", "1.0.0", "1.0.0-rc1"},
			want: "1.0.0",
		},
		{
			name: "v prefix",
			tags: []string{"v1.0.0", "v2.0.0"},
			want: "v2.0.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := klausoci.LatestSemverTag(tt.tags)
			if got != tt.want {
				t.Errorf("LatestSemverTag(%v) = %q, want %q", tt.tags, got, tt.want)
			}
		})
	}
}

func TestListLocalArtifacts_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	artifacts, err := listLocalArtifacts(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(artifacts) != 0 {
		t.Errorf("expected empty list, got %d artifacts", len(artifacts))
	}
}

func TestListLocalArtifacts_NonExistentDir(t *testing.T) {
	artifacts, err := listLocalArtifacts("/nonexistent/path")
	if err != nil {
		t.Fatalf("expected no error for nonexistent dir, got: %v", err)
	}
	if len(artifacts) != 0 {
		t.Errorf("expected empty list, got %d artifacts", len(artifacts))
	}
}

func TestListLocalArtifacts_SkipsFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "not-a-dir.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	artifacts, err := listLocalArtifacts(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(artifacts) != 0 {
		t.Errorf("expected empty list (only files), got %d artifacts", len(artifacts))
	}
}

func TestListLocalArtifacts_SkipsDirsWithoutCache(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "uncached-artifact"), 0o755); err != nil {
		t.Fatal(err)
	}

	artifacts, err := listLocalArtifacts(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(artifacts) != 0 {
		t.Errorf("expected empty list (no cache entries), got %d artifacts", len(artifacts))
	}
}
