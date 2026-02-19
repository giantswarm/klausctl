package oci

import (
	"context"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"

	"github.com/giantswarm/klausctl/pkg/config"
)

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
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ref, nil
	}

	if strings.Contains(ref, "/") {
		if !hasTagOrDigest(ref) {
			return resolveLatestSemver(ctx, ref)
		}
		if hasDigest(ref) {
			return ref, nil
		}
		tag := extractTag(ref)
		if tag != "latest" {
			return ref, nil
		}
		repo := repositoryFromRef(ref)
		return resolveLatestSemver(ctx, repo)
	}

	name, tag := splitNameTag(ref)
	if namePrefix != "" && !strings.HasPrefix(name, namePrefix) {
		name = namePrefix + name
	}
	fullRepo := registryBase + "/" + name

	if tag != "" && tag != "latest" {
		return fullRepo + ":" + tag, nil
	}

	return resolveLatestSemver(ctx, fullRepo)
}

// ResolvePluginRefs resolves a slice of config.Plugin entries, replacing
// "latest" or empty tags with the actual latest semver tag from the registry.
// Plugins with non-"latest" tags or digests are left unchanged.
func ResolvePluginRefs(ctx context.Context, plugins []config.Plugin) ([]config.Plugin, error) {
	resolved := make([]config.Plugin, len(plugins))
	copy(resolved, plugins)

	for i := range resolved {
		if resolved[i].Digest != "" {
			continue
		}
		if resolved[i].Tag != "" && resolved[i].Tag != "latest" {
			continue
		}
		tag, err := resolveLatestTagForRepo(ctx, resolved[i].Repository)
		if err != nil {
			return nil, fmt.Errorf("resolving plugin %s: %w", resolved[i].Repository, err)
		}
		resolved[i].Tag = tag
	}

	return resolved, nil
}

func resolveLatestSemver(ctx context.Context, repo string) (string, error) {
	tag, err := resolveLatestTagForRepo(ctx, repo)
	if err != nil {
		return "", err
	}
	return repo + ":" + tag, nil
}

func resolveLatestTagForRepo(ctx context.Context, repo string) (string, error) {
	client := NewDefaultClient()
	tags, err := client.List(ctx, repo)
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

func splitNameTag(ref string) (string, string) {
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
