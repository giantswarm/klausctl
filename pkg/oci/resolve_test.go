package oci

import (
	"testing"

	"github.com/giantswarm/klausctl/pkg/config"
)

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

func TestResolvePluginRefsSkipsDigests(t *testing.T) {
	plugins := []config.Plugin{
		{Repository: "example.com/plugin-a", Digest: "sha256:abc123"},
	}

	// ResolvePluginRefs should not contact the registry for digest-pinned plugins.
	// We pass a valid digest so no network call is made.
	resolved, err := ResolvePluginRefs(t.Context(), plugins)
	if err != nil {
		t.Fatalf("ResolvePluginRefs() error = %v", err)
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

	resolved, err := ResolvePluginRefs(t.Context(), plugins)
	if err != nil {
		t.Fatalf("ResolvePluginRefs() error = %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(resolved))
	}
	if resolved[0].Tag != "v1.2.3" {
		t.Errorf("tag = %q, want unchanged v1.2.3", resolved[0].Tag)
	}
}
