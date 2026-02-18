package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	klausoci "github.com/giantswarm/klaus-oci"

	"github.com/giantswarm/klausctl/pkg/oci"
)

func TestValidateOutputFormat(t *testing.T) {
	if err := validateOutputFormat("text"); err != nil {
		t.Errorf("expected text to be valid, got: %v", err)
	}
	if err := validateOutputFormat("json"); err != nil {
		t.Errorf("expected json to be valid, got: %v", err)
	}
	if err := validateOutputFormat("yaml"); err == nil {
		t.Error("expected yaml to be rejected")
	}
	if err := validateOutputFormat(""); err == nil {
		t.Error("expected empty string to be rejected")
	}
}

func TestListLocalArtifacts(t *testing.T) {
	dir := t.TempDir()

	pluginDir := filepath.Join(dir, "gs-base")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := klausoci.WriteCacheEntry(pluginDir, klausoci.CacheEntry{
		Digest: "sha256:abc123",
		Ref:    "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base:v0.6.0",
	}); err != nil {
		t.Fatal(err)
	}

	artifacts, err := listLocalArtifacts(dir)
	if err != nil {
		t.Fatalf("listLocalArtifacts() error = %v", err)
	}

	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}

	if artifacts[0].Name != "gs-base" {
		t.Errorf("Name = %q, want %q", artifacts[0].Name, "gs-base")
	}
	if artifacts[0].Digest != "sha256:abc123" {
		t.Errorf("Digest = %q, want %q", artifacts[0].Digest, "sha256:abc123")
	}
}

func TestListLocalArtifactsEmpty(t *testing.T) {
	dir := t.TempDir()

	artifacts, err := listLocalArtifacts(dir)
	if err != nil {
		t.Fatalf("listLocalArtifacts() error = %v", err)
	}
	if len(artifacts) != 0 {
		t.Errorf("expected 0 artifacts, got %d", len(artifacts))
	}
}

func TestListLocalArtifactsMissingDir(t *testing.T) {
	artifacts, err := listLocalArtifacts("/nonexistent/path")
	if err != nil {
		t.Fatalf("listLocalArtifacts() error = %v", err)
	}
	if len(artifacts) != 0 {
		t.Errorf("expected 0 artifacts for missing dir, got %d", len(artifacts))
	}
}

func TestListLocalArtifactsSkipsNonDirs(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "not-a-dir.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	artifacts, err := listLocalArtifacts(dir)
	if err != nil {
		t.Fatalf("listLocalArtifacts() error = %v", err)
	}
	if len(artifacts) != 0 {
		t.Errorf("expected 0 artifacts, got %d", len(artifacts))
	}
}

func TestListLocalArtifactsSkipsNoCacheMetadata(t *testing.T) {
	dir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(dir, "stale-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}

	artifacts, err := listLocalArtifacts(dir)
	if err != nil {
		t.Fatalf("listLocalArtifacts() error = %v", err)
	}
	if len(artifacts) != 0 {
		t.Errorf("expected 0 artifacts (no cache metadata), got %d", len(artifacts))
	}
}

func TestPrintLocalArtifactsText(t *testing.T) {
	var buf bytes.Buffer
	artifacts := []cachedArtifact{
		{
			Name:     "gs-base",
			Ref:      "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base:v0.6.0",
			Digest:   "sha256:abcdef1234567890",
			PulledAt: time.Now().Add(-2 * time.Hour),
		},
	}

	if err := printLocalArtifacts(&buf, artifacts, "text"); err != nil {
		t.Fatalf("printLocalArtifacts() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "NAME") {
		t.Error("expected header with NAME column")
	}
	if !strings.Contains(output, "gs-base") {
		t.Error("expected output to contain artifact name")
	}
	if !strings.Contains(output, "DIGEST") {
		t.Error("expected header with DIGEST column")
	}
}

func TestPrintLocalArtifactsJSON(t *testing.T) {
	var buf bytes.Buffer
	artifacts := []cachedArtifact{
		{
			Name:     "gs-base",
			Ref:      "example.com/plugin:v1",
			Digest:   "sha256:abc",
			PulledAt: time.Now(),
		},
	}

	if err := printLocalArtifacts(&buf, artifacts, "json"); err != nil {
		t.Fatalf("printLocalArtifacts() error = %v", err)
	}

	var result []cachedArtifact
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 artifact in JSON, got %d", len(result))
	}
}

func TestPrintEmptyJSON(t *testing.T) {
	var buf bytes.Buffer

	if err := printEmpty(&buf, "json", "hint line 1", "hint line 2"); err != nil {
		t.Fatalf("printEmpty() error = %v", err)
	}

	output := strings.TrimSpace(buf.String())
	if output != "[]" {
		t.Errorf("expected empty JSON array, got: %s", output)
	}
}

func TestPrintEmptyText(t *testing.T) {
	var buf bytes.Buffer

	if err := printEmpty(&buf, "text", "No items found.", "Try pulling first."); err != nil {
		t.Fatalf("printEmpty() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No items found.") {
		t.Error("expected hint line 1")
	}
	if !strings.Contains(output, "Try pulling first.") {
		t.Error("expected hint line 2")
	}
}

func TestShortNameFromRef(t *testing.T) {
	tests := []struct {
		ref  string
		want string
	}{
		{
			ref:  "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base:v0.6.0",
			want: "gs-base",
		},
		{
			ref:  "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base@sha256:abc123",
			want: "gs-base",
		},
		{
			ref:  "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base",
			want: "gs-base",
		},
		{
			ref:  "example.com/plugin:v1",
			want: "plugin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			got := oci.ShortName(repositoryFromRef(tt.ref))
			if got != tt.want {
				t.Errorf("ShortName(repositoryFromRef(%q)) = %q, want %q", tt.ref, got, tt.want)
			}
		})
	}
}

func TestRepositoryFromRef(t *testing.T) {
	tests := []struct {
		ref  string
		want string
	}{
		{
			ref:  "example.com/repo:v1.0.0",
			want: "example.com/repo",
		},
		{
			ref:  "example.com/repo@sha256:abc123",
			want: "example.com/repo",
		},
		{
			ref:  "example.com/repo",
			want: "example.com/repo",
		},
		{
			ref:  "localhost:5000/repo",
			want: "localhost:5000/repo",
		},
		{
			ref:  "localhost:5000/repo:v1.0.0",
			want: "localhost:5000/repo",
		},
		{
			ref:  "localhost:5000/org/repo@sha256:abc123",
			want: "localhost:5000/org/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			got := repositoryFromRef(tt.ref)
			if got != tt.want {
				t.Errorf("repositoryFromRef(%q) = %q, want %q", tt.ref, got, tt.want)
			}
		})
	}
}

func TestLatestSemverTag(t *testing.T) {
	tests := []struct {
		name string
		tags []string
		want string
	}{
		{
			name: "multiple versions",
			tags: []string{"v0.0.1", "v0.0.3", "v0.0.2"},
			want: "v0.0.3",
		},
		{
			name: "single version",
			tags: []string{"v1.0.0"},
			want: "v1.0.0",
		},
		{
			name: "mixed valid and invalid",
			tags: []string{"latest", "v0.0.6", "main", "v0.0.7"},
			want: "v0.0.7",
		},
		{
			name: "no valid semver",
			tags: []string{"latest", "main", "dev"},
			want: "",
		},
		{
			name: "empty",
			tags: nil,
			want: "",
		},
		{
			name: "prerelease lower than release",
			tags: []string{"v1.0.0-rc.1", "v0.9.0"},
			want: "v1.0.0-rc.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := latestSemverTag(tt.tags)
			if got != tt.want {
				t.Errorf("latestSemverTag(%v) = %q, want %q", tt.tags, got, tt.want)
			}
		})
	}
}

func TestPrintRemoteArtifactsText(t *testing.T) {
	var buf bytes.Buffer
	entries := []remoteArtifactEntry{
		{
			Name: "gs-base", Ref: "example.com/plugins/gs-base:v0.0.7",
			Digest: "sha256:abc123def456", PulledAt: time.Now().Add(-2 * time.Hour),
		},
		{
			Name: "gs-sre", Ref: "example.com/plugins/gs-sre:v0.0.7",
			Digest: "sha256:def789abc012",
		},
	}

	if err := printRemoteArtifacts(&buf, entries, "text"); err != nil {
		t.Fatalf("printRemoteArtifacts() error = %v", err)
	}

	output := buf.String()
	for _, col := range []string{"NAME", "REF", "DIGEST", "PULLED"} {
		if !strings.Contains(output, col) {
			t.Errorf("expected header with %s column", col)
		}
	}
	if !strings.Contains(output, "gs-base") {
		t.Error("expected output to contain gs-base")
	}
	if !strings.Contains(output, "sha256:abc123def456") {
		t.Error("expected output to contain digest")
	}
	if !strings.Contains(output, "h ago") {
		t.Error("expected pulled time for cached artifact")
	}
	if !strings.Contains(output, "-") {
		t.Error("expected dash for unpulled artifact")
	}
}

func TestPrintRemoteArtifactsJSON(t *testing.T) {
	var buf bytes.Buffer
	entries := []remoteArtifactEntry{
		{
			Name: "gs-base", Ref: "example.com/plugins/gs-base:v0.0.7",
			Digest: "sha256:abc123",
		},
	}

	if err := printRemoteArtifacts(&buf, entries, "json"); err != nil {
		t.Fatalf("printRemoteArtifacts() error = %v", err)
	}

	var result []remoteArtifactEntry
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 entry in JSON, got %d", len(result))
	}
	if result[0].Ref != "example.com/plugins/gs-base:v0.0.7" {
		t.Errorf("ref = %q, want full ref", result[0].Ref)
	}
	if result[0].Digest != "sha256:abc123" {
		t.Errorf("digest = %q, want %q", result[0].Digest, "sha256:abc123")
	}
}

func TestSplitNameTag(t *testing.T) {
	tests := []struct {
		ref      string
		wantName string
		wantTag  string
	}{
		{"gs-ae", "gs-ae", ""},
		{"gs-ae:v0.0.7", "gs-ae", "v0.0.7"},
		{"my-plugin:latest", "my-plugin", "latest"},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			name, tag := splitNameTag(tt.ref)
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
			if tag != tt.wantTag {
				t.Errorf("tag = %q, want %q", tag, tt.wantTag)
			}
		})
	}
}

func TestResolveArtifactRefFullRef(t *testing.T) {
	ctx := context.Background()
	ref, err := resolveArtifactRef(ctx, "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base:v0.0.7", "gsoci.azurecr.io/giantswarm/klaus-plugins")
	if err != nil {
		t.Fatalf("resolveArtifactRef() error = %v", err)
	}
	if ref != "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base:v0.0.7" {
		t.Errorf("ref = %q, want full ref unchanged", ref)
	}
}

func TestResolveArtifactRefShortWithTag(t *testing.T) {
	ctx := context.Background()
	ref, err := resolveArtifactRef(ctx, "gs-base:v0.0.7", "gsoci.azurecr.io/giantswarm/klaus-plugins")
	if err != nil {
		t.Fatalf("resolveArtifactRef() error = %v", err)
	}
	want := "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base:v0.0.7"
	if ref != want {
		t.Errorf("ref = %q, want %q", ref, want)
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		name     string
		t        time.Time
		contains string
	}{
		{
			name:     "recent",
			t:        time.Now().Add(-30 * time.Second),
			contains: "just now",
		},
		{
			name:     "minutes",
			t:        time.Now().Add(-5 * time.Minute),
			contains: "m ago",
		},
		{
			name:     "hours",
			t:        time.Now().Add(-3 * time.Hour),
			contains: "h ago",
		},
		{
			name:     "days",
			t:        time.Now().Add(-48 * time.Hour),
			contains: "d ago",
		},
		{
			name:     "zero",
			t:        time.Time{},
			contains: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAge(tt.t)
			if !strings.Contains(got, tt.contains) {
				t.Errorf("formatAge() = %q, want to contain %q", got, tt.contains)
			}
		})
	}
}
