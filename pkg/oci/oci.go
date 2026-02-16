package oci

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/giantswarm/klausctl/pkg/config"
)

// PullPlugins pulls all configured plugins to the local plugins directory.
// Each plugin is stored at <pluginsDir>/<shortName>/. Plugins are cached by
// digest and skipped if already up-to-date. Progress messages are written to w.
func PullPlugins(ctx context.Context, plugins []config.Plugin, pluginsDir string, w io.Writer) error {
	client := NewClient()

	for _, plugin := range plugins {
		shortName := ShortPluginName(plugin.Repository)
		destDir := filepath.Join(pluginsDir, shortName)
		ref := buildRef(plugin)

		fmt.Fprintf(w, "  Pulling %s...\n", ref)

		result, err := client.Pull(ctx, ref, destDir)
		if err != nil {
			return fmt.Errorf("pulling plugin %s: %w", ref, err)
		}

		if result.Cached {
			fmt.Fprintf(w, "  %s: up-to-date (%s)\n", shortName, truncateDigest(result.Digest))
		} else {
			fmt.Fprintf(w, "  %s: pulled (%s)\n", shortName, truncateDigest(result.Digest))
		}
	}

	return nil
}

// ShortPluginName extracts the last segment of a repository path.
// e.g. "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-platform" -> "gs-platform"
func ShortPluginName(repository string) string {
	parts := strings.Split(repository, "/")
	return parts[len(parts)-1]
}

// PluginDirs returns the container-internal mount paths for the given plugins.
// Each plugin is mounted at /var/lib/klaus/plugins/<shortName>.
func PluginDirs(plugins []config.Plugin) []string {
	dirs := make([]string, 0, len(plugins))
	for _, p := range plugins {
		dirs = append(dirs, "/var/lib/klaus/plugins/"+ShortPluginName(p.Repository))
	}
	return dirs
}

// buildRef constructs a full OCI reference from a Plugin spec.
func buildRef(p config.Plugin) string {
	ref := p.Repository
	if p.Digest != "" {
		ref += "@" + p.Digest
	} else if p.Tag != "" {
		ref += ":" + p.Tag
	}
	return ref
}

// truncateDigest shortens a digest string for display (e.g. "sha256:abc123...").
func truncateDigest(d string) string {
	if idx := strings.Index(d, ":"); idx >= 0 {
		suffix := d[idx+1:]
		if len(suffix) > 12 {
			return d[:idx+1] + suffix[:12]
		}
	}
	return d
}
