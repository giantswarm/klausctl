package oci

import (
	"context"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/giantswarm/klausctl/pkg/config"
)

// TagLister can list tags for an OCI repository. Implemented by *Client;
// declared as an interface to allow unit testing without network access.
type TagLister interface {
	List(ctx context.Context, repository string) ([]string, error)
}

// ResolveArtifactRef resolves a short artifact name or OCI reference to a
// fully-qualified reference with its latest semver tag from the registry.
//
// If the ref already has a tag other than "latest" (or a digest), it is
// returned as-is. Short names (no "/") are expanded using registryBase and
// namePrefix (e.g. "go" with prefix "klaus-" becomes "klaus-go").
//
// When no tag is provided or the tag is "latest", the registry is queried
// for all tags and the highest semver tag is selected.
func ResolveArtifactRef(ctx context.Context, ref, registryBase, namePrefix string) (string, error) {
	return resolveArtifactRef(ctx, NewDefaultClient(), ref, registryBase, namePrefix)
}

func resolveArtifactRef(ctx context.Context, lister TagLister, ref, registryBase, namePrefix string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ref, nil
	}

	if strings.Contains(ref, "/") {
		if !hasTagOrDigest(ref) {
			return resolveLatestSemver(ctx, lister, ref)
		}
		if hasDigest(ref) {
			return ref, nil
		}
		tag := extractTag(ref)
		if tag != "latest" {
			return ref, nil
		}
		repo := RepositoryFromRef(ref)
		return resolveLatestSemver(ctx, lister, repo)
	}

	name, tag := SplitNameTag(ref)
	if namePrefix != "" && !strings.HasPrefix(name, namePrefix) {
		name = namePrefix + name
	}
	fullRepo := registryBase + "/" + name

	if tag != "" && tag != "latest" {
		return fullRepo + ":" + tag, nil
	}

	return resolveLatestSemver(ctx, lister, fullRepo)
}

// ResolvePluginRefs resolves a slice of config.Plugin entries, replacing
// "latest" or empty tags with the actual latest semver tag from the registry.
// Plugins with non-"latest" tags or digests are left unchanged.
func ResolvePluginRefs(ctx context.Context, plugins []config.Plugin) ([]config.Plugin, error) {
	return resolvePluginRefs(ctx, NewDefaultClient(), plugins)
}

func resolvePluginRefs(ctx context.Context, lister TagLister, plugins []config.Plugin) ([]config.Plugin, error) {
	resolved := make([]config.Plugin, len(plugins))
	copy(resolved, plugins)

	for i := range resolved {
		if resolved[i].Digest != "" {
			continue
		}
		if resolved[i].Tag != "" && resolved[i].Tag != "latest" {
			continue
		}
		tag, err := resolveLatestTagForRepo(ctx, lister, resolved[i].Repository)
		if err != nil {
			return nil, fmt.Errorf("resolving plugin %s: %w", resolved[i].Repository, err)
		}
		resolved[i].Tag = tag
	}

	return resolved, nil
}

// ResolveCreateRefs resolves personality, toolchain, and plugin short names
// to full OCI references with proper semver tags from the registry.
func ResolveCreateRefs(ctx context.Context, personality, toolchain string, plugins []string) (string, string, []string, error) {
	client := NewDefaultClient()

	if personality != "" {
		ref, err := resolveArtifactRef(ctx, client, personality, DefaultPersonalityRegistry, "")
		if err != nil {
			return "", "", nil, fmt.Errorf("resolving personality: %w", err)
		}
		personality = ref
	}

	if toolchain != "" {
		ref, err := resolveArtifactRef(ctx, client, toolchain, DefaultToolchainRegistry, "klaus-")
		if err != nil {
			return "", "", nil, fmt.Errorf("resolving toolchain: %w", err)
		}
		toolchain = ref
	}

	resolved := make([]string, 0, len(plugins))
	for _, p := range plugins {
		ref, err := resolveArtifactRef(ctx, client, p, DefaultPluginRegistry, "")
		if err != nil {
			return "", "", nil, fmt.Errorf("resolving plugin: %w", err)
		}
		resolved = append(resolved, ref)
	}

	return personality, toolchain, resolved, nil
}

// PluginRefsFromSpec converts personality spec plugin references to config.Plugin entries.
func PluginRefsFromSpec(refs []PluginReference) []config.Plugin {
	plugins := make([]config.Plugin, 0, len(refs))
	for _, p := range refs {
		plugins = append(plugins, PluginFromReference(p))
	}
	return plugins
}

func resolveLatestSemver(ctx context.Context, lister TagLister, repo string) (string, error) {
	tag, err := resolveLatestTagForRepo(ctx, lister, repo)
	if err != nil {
		return "", err
	}
	return repo + ":" + tag, nil
}

func resolveLatestTagForRepo(ctx context.Context, lister TagLister, repo string) (string, error) {
	tags, err := lister.List(ctx, repo)
	if err != nil {
		return "", fmt.Errorf("listing tags for %s: %w", repo, err)
	}

	latest := LatestSemverTag(tags)
	if latest == "" {
		return "", fmt.Errorf("no semver tags found for %s", repo)
	}

	return latest, nil
}

// LatestSemverTag returns the highest semver tag from the given list.
// Tags that are not valid semver are ignored.
func LatestSemverTag(tags []string) string {
	var best *semver.Version
	var bestTag string

	for _, tag := range tags {
		v, err := semver.NewVersion(tag)
		if err != nil {
			continue
		}
		if best == nil || v.GreaterThan(best) {
			best = v
			bestTag = tag
		}
	}

	return bestTag
}

// SplitNameTag splits "name:tag" into name and tag. If no colon is present,
// tag is empty.
func SplitNameTag(ref string) (string, string) {
	if idx := strings.LastIndex(ref, ":"); idx >= 0 {
		return ref[:idx], ref[idx+1:]
	}
	return ref, ""
}

func hasTagOrDigest(ref string) bool {
	if hasDigest(ref) {
		return true
	}
	nameStart := strings.LastIndex(ref, "/")
	tagIdx := strings.LastIndex(ref, ":")
	return tagIdx > nameStart
}

func hasDigest(ref string) bool {
	return strings.Contains(ref, "@")
}

func extractTag(ref string) string {
	if hasDigest(ref) {
		return ""
	}
	nameStart := strings.LastIndex(ref, "/")
	tagIdx := strings.LastIndex(ref, ":")
	if tagIdx > nameStart {
		return ref[tagIdx+1:]
	}
	return ""
}
