package oci

import (
	"context"
	"fmt"
	"testing"

	"github.com/giantswarm/klausctl/pkg/config"
)

// mockTagLister returns preconfigured tag lists keyed by repository.
type mockTagLister struct {
	tags map[string][]string
}

func (m *mockTagLister) List(_ context.Context, repository string) ([]string, error) {
	tags, ok := m.tags[repository]
	if !ok {
		return nil, fmt.Errorf("repository not found: %s", repository)
	}
	return tags, nil
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
			got := LatestSemverTag(tt.tags)
			if got != tt.want {
				t.Errorf("LatestSemverTag(%v) = %q, want %q", tt.tags, got, tt.want)
			}
		})
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
			name, tag := SplitNameTag(tt.ref)
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
			if tag != tt.wantTag {
				t.Errorf("tag = %q, want %q", tag, tt.wantTag)
			}
		})
	}
}

func TestHasTagOrDigest(t *testing.T) {
	tests := []struct {
		ref  string
		want bool
	}{
		{"example.com/repo:v1.0.0", true},
		{"example.com/repo@sha256:abc123", true},
		{"example.com/repo", false},
		{"localhost:5000/repo", false},
		{"localhost:5000/repo:v1.0.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			got := hasTagOrDigest(tt.ref)
			if got != tt.want {
				t.Errorf("hasTagOrDigest(%q) = %v, want %v", tt.ref, got, tt.want)
			}
		})
	}
}

func TestExtractTag(t *testing.T) {
	tests := []struct {
		ref  string
		want string
	}{
		{"example.com/repo:v1.0.0", "v1.0.0"},
		{"example.com/repo:latest", "latest"},
		{"example.com/repo@sha256:abc123", ""},
		{"example.com/repo", ""},
		{"localhost:5000/repo:v1.0.0", "v1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			got := extractTag(tt.ref)
			if got != tt.want {
				t.Errorf("extractTag(%q) = %q, want %q", tt.ref, got, tt.want)
			}
		})
	}
}

func TestResolveArtifactRef(t *testing.T) {
	lister := &mockTagLister{
		tags: map[string][]string{
			"gsoci.azurecr.io/giantswarm/klaus-plugins/gs-ae":        {"v0.0.1", "v0.0.3", "v0.0.2"},
			"gsoci.azurecr.io/giantswarm/klaus-go":                   {"v1.0.0", "v1.1.0"},
			"gsoci.azurecr.io/giantswarm/klaus-personalities/sre":    {"v0.1.0", "v0.2.0"},
			"custom.registry.io/org/my-plugin":                       {"v2.0.0"},
		},
	}
	ctx := context.Background()

	tests := []struct {
		name         string
		ref          string
		registryBase string
		namePrefix   string
		want         string
	}{
		{
			name:         "empty ref",
			ref:          "",
			registryBase: "gsoci.azurecr.io/giantswarm/klaus-plugins",
			want:         "",
		},
		{
			name:         "short name with explicit tag",
			ref:          "gs-ae:v0.0.2",
			registryBase: "gsoci.azurecr.io/giantswarm/klaus-plugins",
			want:         "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-ae:v0.0.2",
		},
		{
			name:         "short name without tag resolves latest",
			ref:          "gs-ae",
			registryBase: "gsoci.azurecr.io/giantswarm/klaus-plugins",
			want:         "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-ae:v0.0.3",
		},
		{
			name:         "short name with latest tag resolves actual",
			ref:          "gs-ae:latest",
			registryBase: "gsoci.azurecr.io/giantswarm/klaus-plugins",
			want:         "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-ae:v0.0.3",
		},
		{
			name:         "short name with prefix",
			ref:          "go",
			registryBase: "gsoci.azurecr.io/giantswarm",
			namePrefix:   "klaus-",
			want:         "gsoci.azurecr.io/giantswarm/klaus-go:v1.1.0",
		},
		{
			name:         "short name already has prefix",
			ref:          "klaus-go",
			registryBase: "gsoci.azurecr.io/giantswarm",
			namePrefix:   "klaus-",
			want:         "gsoci.azurecr.io/giantswarm/klaus-go:v1.1.0",
		},
		{
			name:         "full ref with tag returned as-is",
			ref:          "custom.registry.io/org/my-plugin:v2.0.0",
			registryBase: "gsoci.azurecr.io/giantswarm/klaus-plugins",
			want:         "custom.registry.io/org/my-plugin:v2.0.0",
		},
		{
			name:         "full ref with digest returned as-is",
			ref:          "custom.registry.io/org/my-plugin@sha256:abc123",
			registryBase: "gsoci.azurecr.io/giantswarm/klaus-plugins",
			want:         "custom.registry.io/org/my-plugin@sha256:abc123",
		},
		{
			name:         "full ref without tag resolves latest",
			ref:          "custom.registry.io/org/my-plugin",
			registryBase: "gsoci.azurecr.io/giantswarm/klaus-plugins",
			want:         "custom.registry.io/org/my-plugin:v2.0.0",
		},
		{
			name:         "full ref with latest tag resolves actual",
			ref:          "custom.registry.io/org/my-plugin:latest",
			registryBase: "gsoci.azurecr.io/giantswarm/klaus-plugins",
			want:         "custom.registry.io/org/my-plugin:v2.0.0",
		},
		{
			name:         "whitespace trimmed",
			ref:          "  gs-ae:v0.0.2  ",
			registryBase: "gsoci.azurecr.io/giantswarm/klaus-plugins",
			want:         "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-ae:v0.0.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveArtifactRef(ctx, lister, tt.ref, tt.registryBase, tt.namePrefix)
			if err != nil {
				t.Fatalf("resolveArtifactRef() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("resolveArtifactRef() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvePluginRefsSkipsDigests(t *testing.T) {
	plugins := []config.Plugin{
		{Repository: "example.com/plugin-a", Digest: "sha256:abc123"},
	}

	lister := &mockTagLister{tags: map[string][]string{}}
	resolved, err := resolvePluginRefs(t.Context(), lister, plugins)
	if err != nil {
		t.Fatalf("resolvePluginRefs() error = %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(resolved))
	}
	if resolved[0].Digest != "sha256:abc123" {
		t.Errorf("digest = %q, want unchanged", resolved[0].Digest)
	}
}

func TestResolvePluginRefsSkipsVersionedTags(t *testing.T) {
	plugins := []config.Plugin{
		{Repository: "example.com/plugin-a", Tag: "v1.2.3"},
	}

	lister := &mockTagLister{tags: map[string][]string{}}
	resolved, err := resolvePluginRefs(t.Context(), lister, plugins)
	if err != nil {
		t.Fatalf("resolvePluginRefs() error = %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(resolved))
	}
	if resolved[0].Tag != "v1.2.3" {
		t.Errorf("tag = %q, want unchanged v1.2.3", resolved[0].Tag)
	}
}

func TestResolvePluginRefsResolvesLatest(t *testing.T) {
	plugins := []config.Plugin{
		{Repository: "example.com/plugin-a", Tag: "latest"},
		{Repository: "example.com/plugin-b"},
	}

	lister := &mockTagLister{
		tags: map[string][]string{
			"example.com/plugin-a": {"v1.0.0", "v1.1.0"},
			"example.com/plugin-b": {"v0.5.0"},
		},
	}
	resolved, err := resolvePluginRefs(t.Context(), lister, plugins)
	if err != nil {
		t.Fatalf("resolvePluginRefs() error = %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(resolved))
	}
	if resolved[0].Tag != "v1.1.0" {
		t.Errorf("plugin-a tag = %q, want v1.1.0", resolved[0].Tag)
	}
	if resolved[1].Tag != "v0.5.0" {
		t.Errorf("plugin-b tag = %q, want v0.5.0", resolved[1].Tag)
	}
}
