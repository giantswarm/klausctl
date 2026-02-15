// Package oci handles pulling OCI plugin artifacts from registries using ORAS.
//
// Plugins are OCI artifacts containing skills, hooks, agents, and MCP server
// configurations. They are pulled to the local plugins directory before
// the klaus container is started, then bind-mounted into the container.
//
// TODO(klausctl#4): Implement ORAS-based plugin pulling.
// This is currently a placeholder that creates the plugin directory structure.
package oci

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/giantswarm/klausctl/pkg/config"
)

// PullPlugins pulls all configured plugins to the local plugins directory.
// Each plugin is stored at <pluginsDir>/<shortName>/.
// Progress messages are written to w.
func PullPlugins(plugins []config.Plugin, pluginsDir string, w io.Writer) error {
	for _, plugin := range plugins {
		shortName := ShortPluginName(plugin.Repository)
		destDir := filepath.Join(pluginsDir, shortName)

		if err := config.EnsureDir(destDir); err != nil {
			return fmt.Errorf("creating plugin directory %q: %w", destDir, err)
		}

		ref := plugin.Repository
		if plugin.Digest != "" {
			ref += "@" + plugin.Digest
		} else if plugin.Tag != "" {
			ref += ":" + plugin.Tag
		}

		// TODO: Implement actual ORAS pull.
		// For now, log the intent.
		fmt.Fprintf(w, "  Plugin: %s -> %s (ORAS pull not yet implemented)\n", ref, destDir)
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
// Each plugin is mounted at /mnt/plugins/<shortName>.
func PluginDirs(plugins []config.Plugin) []string {
	dirs := make([]string, 0, len(plugins))
	for _, p := range plugins {
		dirs = append(dirs, "/mnt/plugins/"+ShortPluginName(p.Repository))
	}
	return dirs
}
