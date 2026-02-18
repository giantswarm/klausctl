package oci

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/giantswarm/klausctl/pkg/config"
)

// RegistryAuthEnvVar is the environment variable checked for base64-encoded
// Docker config JSON credentials. This supplements the default Docker/Podman
// config file resolution and is primarily useful in CI/headless environments
// where `az acr login` is not available.
const RegistryAuthEnvVar = "KLAUSCTL_REGISTRY_AUTH"

// NewDefaultClient creates an OCI client configured with the standard
// klausctl credential resolution: Docker/Podman config files plus the
// KLAUSCTL_REGISTRY_AUTH environment variable.
func NewDefaultClient(opts ...ClientOption) *Client {
	return NewClient(append([]ClientOption{WithRegistryAuthEnv(RegistryAuthEnvVar)}, opts...)...)
}

// PullPlugins pulls all configured plugins to the local plugins directory.
// Each plugin is stored at <pluginsDir>/<shortName>/. Plugins are cached by
// digest and skipped if already up-to-date. Progress messages are written to w.
func PullPlugins(ctx context.Context, plugins []config.Plugin, pluginsDir string, w io.Writer) error {
	client := NewDefaultClient()

	for _, plugin := range plugins {
		shortName := ShortPluginName(plugin.Repository)
		destDir := filepath.Join(pluginsDir, shortName)
		ref := BuildRef(plugin)

		fmt.Fprintf(w, "  Pulling %s...\n", ref)

		result, err := client.Pull(ctx, ref, destDir, PluginArtifact)
		if err != nil {
			return fmt.Errorf("pulling plugin %s: %w", ref, err)
		}

		if result.Cached {
			fmt.Fprintf(w, "  %s: up-to-date (%s)\n", shortName, TruncateDigest(result.Digest))
		} else {
			fmt.Fprintf(w, "  %s: pulled (%s)\n", shortName, TruncateDigest(result.Digest))
		}
	}

	return nil
}

// ShortPluginName extracts the last segment of a repository path.
// e.g. "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-platform" -> "gs-platform"
func ShortPluginName(repository string) string {
	return ShortName(repository)
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

// BuildRef constructs a full OCI reference from a Plugin spec.
func BuildRef(p config.Plugin) string {
	ref := p.Repository
	if p.Digest != "" {
		ref += "@" + p.Digest
	} else if p.Tag != "" {
		ref += ":" + p.Tag
	}
	return ref
}

// DefaultRegistries defines the default OCI registry base paths for each
// artifact type, used by the list --remote commands.
const (
	DefaultPluginRegistry      = "gsoci.azurecr.io/giantswarm/klaus-plugins"
	DefaultPersonalityRegistry = "gsoci.azurecr.io/giantswarm/klaus-personalities"
	DefaultToolchainRegistry   = "gsoci.azurecr.io/giantswarm"
)

// ToolchainRegistryRef returns the full registry reference for a toolchain
// image name. Toolchains use the pattern gsoci.azurecr.io/giantswarm/klaus-<name>.
func ToolchainRegistryRef(name string) string {
	if strings.HasPrefix(name, DefaultToolchainRegistry) {
		return name
	}
	return DefaultToolchainRegistry + "/klaus-" + name
}
