package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	klausoci "github.com/giantswarm/klaus-oci"
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

func TestPushArtifactText(t *testing.T) {
	var buf bytes.Buffer
	fakePush := func(_ context.Context, _ *klausoci.Client, _, _ string) (string, error) {
		return "sha256:deadbeef12345678", nil
	}

	err := pushArtifact(context.Background(), "/tmp/src", "example.com/plugins/gs-base:v1.0.0", fakePush, &buf, "text", pushOpts{})
	if err != nil {
		t.Fatalf("pushArtifact() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "gs-base") {
		t.Error("expected output to contain short name")
	}
	if !strings.Contains(output, "pushed") {
		t.Error("expected output to contain 'pushed'")
	}
	if !strings.Contains(output, "sha256:deadbeef1234") {
		t.Error("expected output to contain truncated digest")
	}
}

func TestPushArtifactJSON(t *testing.T) {
	var buf bytes.Buffer
	fakePush := func(_ context.Context, _ *klausoci.Client, _, _ string) (string, error) {
		return "sha256:deadbeef12345678", nil
	}

	err := pushArtifact(context.Background(), "/tmp/src", "example.com/plugins/gs-base:v1.0.0", fakePush, &buf, "json", pushOpts{})
	if err != nil {
		t.Fatalf("pushArtifact() error = %v", err)
	}

	var result pushResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if result.Name != "gs-base" {
		t.Errorf("Name = %q, want %q", result.Name, "gs-base")
	}
	if result.Ref != "example.com/plugins/gs-base:v1.0.0" {
		t.Errorf("Ref = %q, want full ref", result.Ref)
	}
	if result.Digest != "sha256:deadbeef12345678" {
		t.Errorf("Digest = %q, want %q", result.Digest, "sha256:deadbeef12345678")
	}
}

func TestPushArtifactError(t *testing.T) {
	var buf bytes.Buffer
	fakePush := func(_ context.Context, _ *klausoci.Client, _, _ string) (string, error) {
		return "", fmt.Errorf("registry unavailable")
	}

	err := pushArtifact(context.Background(), "/tmp/src", "example.com/plugins/gs-base:v1.0.0", fakePush, &buf, "text", pushOpts{})
	if err == nil {
		t.Fatal("expected error from push")
	}
	if !strings.Contains(err.Error(), "registry unavailable") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPushArtifactDryRunText(t *testing.T) {
	var buf bytes.Buffer
	pushCalled := false
	fakePush := func(_ context.Context, _ *klausoci.Client, _, _ string) (string, error) {
		pushCalled = true
		return "sha256:deadbeef12345678", nil
	}

	err := pushArtifact(context.Background(), "/tmp/src", "example.com/plugins/gs-base:v1.0.0", fakePush, &buf, "text", pushOpts{dryRun: true})
	if err != nil {
		t.Fatalf("pushArtifact() error = %v", err)
	}
	if pushCalled {
		t.Error("push function should not be called in dry-run mode")
	}

	output := buf.String()
	if !strings.Contains(output, "dry run") {
		t.Error("expected output to mention dry run")
	}
	if !strings.Contains(output, "gs-base") {
		t.Error("expected output to contain short name")
	}
}

func TestPushArtifactDryRunJSON(t *testing.T) {
	var buf bytes.Buffer
	fakePush := func(_ context.Context, _ *klausoci.Client, _, _ string) (string, error) {
		t.Fatal("push function should not be called in dry-run mode")
		return "", nil
	}

	err := pushArtifact(context.Background(), "/tmp/src", "example.com/plugins/gs-base:v1.0.0", fakePush, &buf, "json", pushOpts{dryRun: true})
	if err != nil {
		t.Fatalf("pushArtifact() error = %v", err)
	}

	var result pushResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("JSON parse error: %v", err)
	}
	if !result.DryRun {
		t.Error("expected DryRun to be true")
	}
	if result.Digest != "" {
		t.Errorf("expected empty Digest in dry run, got %q", result.Digest)
	}
}

func TestValidatePushRef(t *testing.T) {
	tests := []struct {
		ref     string
		wantErr bool
	}{
		{ref: "example.com/plugins/gs-base:v1.0.0", wantErr: false},
		{ref: "gs-base:v1.0.0", wantErr: false},
		{ref: "example.com/plugins/gs-base", wantErr: true},
		{ref: "gs-base", wantErr: true},
		{ref: "localhost:5000/plugins/gs-base", wantErr: true},
		{ref: "localhost:5000/plugins/gs-base:v1.0.0", wantErr: false},
		{ref: "registry.example.com:5000/repo", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			err := validatePushRef(tt.ref)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePushRef(%q) error = %v, wantErr %v", tt.ref, err, tt.wantErr)
			}
		})
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
			got := klausoci.ShortName(klausoci.RepositoryFromRef(tt.ref))
			if got != tt.want {
				t.Errorf("ShortName(RepositoryFromRef(%q)) = %q, want %q", tt.ref, got, tt.want)
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
			got := klausoci.LatestSemverTag(tt.tags)
			if got != tt.want {
				t.Errorf("LatestSemverTag(%v) = %q, want %q", tt.tags, got, tt.want)
			}
		})
	}
}

func TestPrintRemoteArtifactsText(t *testing.T) {
	var buf bytes.Buffer
	entries := []remoteArtifactEntry{
		{
			Name: "gs-base", Ref: "example.com/plugins/gs-base:v0.0.7",
			PulledAt: time.Now().Add(-2 * time.Hour),
		},
		{
			Name: "gs-sre", Ref: "example.com/plugins/gs-sre:v0.0.7",
		},
	}

	if err := printRemoteArtifacts(&buf, entries, "text"); err != nil {
		t.Fatalf("printRemoteArtifacts() error = %v", err)
	}

	output := buf.String()
	for _, col := range []string{"NAME", "REF", "PULLED"} {
		if !strings.Contains(output, col) {
			t.Errorf("expected header with %s column", col)
		}
	}
	if !strings.Contains(output, "gs-base") {
		t.Error("expected output to contain gs-base")
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
}

func TestPrintArtifactMetaFull(t *testing.T) {
	var buf bytes.Buffer
	printArtifactMeta(&buf, artifactMeta{
		Name:        "gs-base",
		Version:     "v0.1.0",
		Description: "Base plugin for Giant Swarm",
		Author:      "Giant Swarm <support@giantswarm.io>",
		Homepage:    "https://giantswarm.io",
		Repository:  "https://github.com/giantswarm/gs-base",
		License:     "Apache-2.0",
		Keywords:    []string{"kubernetes", "platform"},
		Digest:      "sha256:abc123def456",
	})

	output := buf.String()
	for _, want := range []string{
		"Name:          gs-base",
		"Version:       v0.1.0",
		"Description:   Base plugin for Giant Swarm",
		"Author:        Giant Swarm <support@giantswarm.io>",
		"Homepage:      https://giantswarm.io",
		"Repository:    https://github.com/giantswarm/gs-base",
		"License:       Apache-2.0",
		"Keywords:      kubernetes, platform",
		"Digest:        sha256:abc123def456",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, output)
		}
	}
}

func TestPrintArtifactMetaMinimal(t *testing.T) {
	var buf bytes.Buffer
	printArtifactMeta(&buf, artifactMeta{
		Name: "minimal",
	})

	output := buf.String()
	if !strings.Contains(output, "Name:") {
		t.Error("expected Name field")
	}
	for _, field := range []string{"Version:", "Description:", "Author:", "Homepage:", "Repository:", "License:", "Keywords:", "Digest:"} {
		if strings.Contains(output, field) {
			t.Errorf("expected empty field %q to be omitted", field)
		}
	}
}

func TestFormatAuthor(t *testing.T) {
	tests := []struct {
		name   string
		author *klausoci.Author
		want   string
	}{
		{name: "nil", author: nil, want: ""},
		{name: "name only", author: &klausoci.Author{Name: "GS"}, want: "GS"},
		{name: "name and email", author: &klausoci.Author{Name: "GS", Email: "hi@gs.io"}, want: "GS <hi@gs.io>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAuthor(tt.author)
			if got != tt.want {
				t.Errorf("formatAuthor() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMetaFromPlugin(t *testing.T) {
	dp := &klausoci.DescribedPlugin{
		ArtifactInfo: klausoci.ArtifactInfo{Digest: "sha256:abc"},
		Plugin: klausoci.Plugin{
			Name:        "gs-base",
			Version:     "v0.1.0",
			Description: "desc",
			Author:      &klausoci.Author{Name: "GS", Email: "e@gs.io"},
			License:     "MIT",
		},
	}
	m := metaFromPlugin(dp)
	if m.Name != "gs-base" {
		t.Errorf("Name = %q", m.Name)
	}
	if m.Author != "GS <e@gs.io>" {
		t.Errorf("Author = %q", m.Author)
	}
	if m.Digest != "sha256:abc" {
		t.Errorf("Digest = %q", m.Digest)
	}
}

func TestMetaFromPersonality(t *testing.T) {
	dp := &klausoci.DescribedPersonality{
		ArtifactInfo: klausoci.ArtifactInfo{Digest: "sha256:def"},
		Personality: klausoci.Personality{
			Name:    "sre",
			Version: "v0.2.0",
			Author:  &klausoci.Author{Name: "GS"},
		},
	}
	m := metaFromPersonality(dp)
	if m.Name != "sre" {
		t.Errorf("Name = %q", m.Name)
	}
	if m.Version != "v0.2.0" {
		t.Errorf("Version = %q", m.Version)
	}
}

func TestMetaFromToolchain(t *testing.T) {
	dt := &klausoci.DescribedToolchain{
		ArtifactInfo: klausoci.ArtifactInfo{Digest: "sha256:789"},
		Toolchain: klausoci.Toolchain{
			Name:    "go",
			Version: "v1.0.0",
		},
	}
	m := metaFromToolchain(dt)
	if m.Name != "go" {
		t.Errorf("Name = %q", m.Name)
	}
	if m.Version != "v1.0.0" {
		t.Errorf("Version = %q", m.Version)
	}
}

func TestNewDescribePluginJSON(t *testing.T) {
	dp := &klausoci.DescribedPlugin{
		ArtifactInfo: klausoci.ArtifactInfo{Ref: "example.com/p:v1", Digest: "sha256:abc"},
		Plugin: klausoci.Plugin{
			Name:       "gs-base",
			Version:    "v0.1.0",
			Skills:     []string{"k8s", "flux"},
			MCPServers: []string{"github"},
			HasHooks:   true,
		},
	}
	j := newDescribePluginJSON(dp)
	if j.Name != "gs-base" {
		t.Errorf("Name = %q", j.Name)
	}
	if j.Ref != "example.com/p:v1" {
		t.Errorf("Ref = %q", j.Ref)
	}
	if len(j.Skills) != 2 {
		t.Errorf("Skills = %v", j.Skills)
	}
	if !j.HasHooks {
		t.Error("expected HasHooks=true")
	}
}

func TestNewDescribePersonalityJSON(t *testing.T) {
	dp := &klausoci.DescribedPersonality{
		ArtifactInfo: klausoci.ArtifactInfo{Ref: "example.com/p:v1", Digest: "sha256:def"},
		Personality: klausoci.Personality{
			Name:      "sre",
			Version:   "v0.2.0",
			Toolchain: klausoci.ToolchainReference{Repository: "example.com/tc", Tag: "v1.0.0"},
			Plugins: []klausoci.PluginReference{
				{Repository: "example.com/p1", Tag: "v0.1.0"},
			},
		},
	}
	j := newDescribePersonalityJSON(dp, nil)
	if j.Toolchain != "example.com/tc:v1.0.0" {
		t.Errorf("Toolchain = %q", j.Toolchain)
	}
	if len(j.Plugins) != 1 {
		t.Errorf("Plugins = %v", j.Plugins)
	}
	if j.ResolvedDeps != nil {
		t.Error("expected nil ResolvedDeps without deps")
	}
}

func TestNewDescribePersonalityJSONWithDeps(t *testing.T) {
	dp := &klausoci.DescribedPersonality{
		ArtifactInfo: klausoci.ArtifactInfo{Ref: "example.com/p:v1", Digest: "sha256:def"},
		Personality: klausoci.Personality{
			Name:    "sre",
			Version: "v0.2.0",
		},
	}
	deps := &klausoci.ResolvedDependencies{
		Toolchain: &klausoci.DescribedToolchain{
			ArtifactInfo: klausoci.ArtifactInfo{Ref: "example.com/tc:v1", Digest: "sha256:tc"},
			Toolchain:    klausoci.Toolchain{Name: "go", Version: "v1.0.0"},
		},
		Plugins: []klausoci.DescribedPlugin{
			{
				ArtifactInfo: klausoci.ArtifactInfo{Ref: "example.com/p1:v1", Digest: "sha256:p1"},
				Plugin:       klausoci.Plugin{Name: "gs-base", Version: "v0.1.0"},
			},
		},
		Warnings: []string{"plugin gs-sre: not found"},
	}
	j := newDescribePersonalityJSON(dp, deps)
	if j.ResolvedDeps == nil {
		t.Fatal("expected ResolvedDeps")
	}
	if j.ResolvedDeps.Toolchain == nil {
		t.Fatal("expected ResolvedDeps.Toolchain")
	}
	if j.ResolvedDeps.Toolchain.Name != "go" {
		t.Errorf("Toolchain.Name = %q", j.ResolvedDeps.Toolchain.Name)
	}
	if len(j.ResolvedDeps.Plugins) != 1 {
		t.Errorf("Plugins = %d", len(j.ResolvedDeps.Plugins))
	}
	if len(j.ResolvedDeps.Warnings) != 1 {
		t.Errorf("Warnings = %v", j.ResolvedDeps.Warnings)
	}
}

func TestNewDescribeToolchainJSON(t *testing.T) {
	dt := &klausoci.DescribedToolchain{
		ArtifactInfo: klausoci.ArtifactInfo{Ref: "example.com/tc:v1", Digest: "sha256:789"},
		Toolchain: klausoci.Toolchain{
			Name:    "go",
			Version: "v1.0.0",
			Author:  &klausoci.Author{Name: "GS"},
		},
	}
	j := newDescribeToolchainJSON(dt)
	if j.Name != "go" {
		t.Errorf("Name = %q", j.Name)
	}
	if j.Version != "v1.0.0" {
		t.Errorf("Version = %q", j.Version)
	}
	if j.Author != "GS" {
		t.Errorf("Author = %q", j.Author)
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
