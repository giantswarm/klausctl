// Package oci wraps the shared giantswarm/klaus-oci library and adds
// klausctl-specific helpers such as CLI cache paths, plugin directory
// resolution, and container mount path computation.
package oci

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	klausoci "github.com/giantswarm/klaus-oci"
	"gopkg.in/yaml.v3"

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
func NewDefaultClient(opts ...klausoci.ClientOption) *klausoci.Client {
	return klausoci.NewClient(append([]klausoci.ClientOption{klausoci.WithRegistryAuthEnv(RegistryAuthEnvVar)}, opts...)...)
}

// ResolveCreateRefs resolves personality, toolchain, and plugin short names
// to full OCI references with proper semver tags from the registry.
func ResolveCreateRefs(ctx context.Context, personality, toolchain string, plugins []string) (string, string, []string, error) {
	client := NewDefaultClient()

	if personality != "" {
		ref, err := client.ResolvePersonalityRef(ctx, personality)
		if err != nil {
			return "", "", nil, fmt.Errorf("resolving personality: %w", err)
		}
		personality = ref
	}

	if toolchain != "" {
		ref, err := client.ResolveToolchainRef(ctx, toolchain)
		if err != nil {
			return "", "", nil, fmt.Errorf("resolving toolchain: %w", err)
		}
		toolchain = ref
	}

	resolved := make([]string, 0, len(plugins))
	for _, p := range plugins {
		ref, err := client.ResolvePluginRef(ctx, p)
		if err != nil {
			return "", "", nil, fmt.Errorf("resolving plugin: %w", err)
		}
		resolved = append(resolved, ref)
	}

	return personality, toolchain, resolved, nil
}

// ResolvePluginRefs resolves a slice of PluginReference entries, replacing
// "latest" or empty tags with the actual latest semver tag from the registry.
// The resolved references are returned as config.Plugin entries.
func ResolvePluginRefs(ctx context.Context, refs []klausoci.PluginReference) ([]config.Plugin, error) {
	client := NewDefaultClient()
	resolved, err := client.ResolvePluginRefs(ctx, refs)
	if err != nil {
		return nil, err
	}
	plugins := make([]config.Plugin, len(resolved))
	for i, r := range resolved {
		plugins[i] = PluginFromReference(r)
	}
	return plugins, nil
}

// PullPlugins pulls all configured plugins to the local plugins directory.
// Each plugin is stored at <pluginsDir>/<shortName>/. Plugins are cached by
// digest and skipped if already up-to-date. Progress messages are written to w.
//
// Plugins with a "latest" tag or no tag are resolved to the latest semver
// tag from the registry before pulling.
func PullPlugins(ctx context.Context, plugins []config.Plugin, pluginsDir string, w io.Writer) error {
	client := NewDefaultClient()

	refs := make([]klausoci.PluginReference, len(plugins))
	for i, p := range plugins {
		refs[i] = klausoci.PluginReference{
			Repository: p.Repository,
			Tag:        p.Tag,
			Digest:     p.Digest,
		}
	}

	resolved, err := client.ResolvePluginRefs(ctx, refs)
	if err != nil {
		return err
	}

	for _, ref := range resolved {
		shortName := klausoci.ShortName(ref.Repository)
		destDir := filepath.Join(pluginsDir, shortName)
		refStr := ref.Ref()

		fmt.Fprintf(w, "  Pulling %s...\n", refStr)

		result, err := client.Pull(ctx, refStr, destDir, klausoci.PluginArtifact)
		if err != nil {
			return fmt.Errorf("pulling plugin %s: %w", refStr, err)
		}

		if result.Cached {
			fmt.Fprintf(w, "  %s: up-to-date (%s)\n", shortName, klausoci.TruncateDigest(result.Digest))
		} else {
			fmt.Fprintf(w, "  %s: pulled (%s)\n", shortName, klausoci.TruncateDigest(result.Digest))
		}
	}

	return nil
}

// ShortPluginName extracts the last segment of a repository path.
// e.g. "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-platform" -> "gs-platform"
func ShortPluginName(repository string) string {
	return klausoci.ShortName(repository)
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

// PersonalityResult holds the outcome of resolving a personality artifact.
type PersonalityResult struct {
	// Spec is the parsed personality.yaml content.
	Spec klausoci.PersonalitySpec
	// Dir is the local directory where the personality was pulled.
	Dir string
	// ShortName is the short name extracted from the OCI reference.
	ShortName string
}

// ResolvePersonality pulls a personality OCI artifact and parses its spec.
// The personality is stored at <personalitiesDir>/<shortName>/.
func ResolvePersonality(ctx context.Context, ref, personalitiesDir string, w io.Writer) (*PersonalityResult, error) {
	repo := klausoci.RepositoryFromRef(ref)
	shortName := klausoci.ShortName(repo)
	destDir := filepath.Join(personalitiesDir, shortName)

	client := NewDefaultClient()

	fmt.Fprintf(w, "  Pulling personality %s...\n", ref)
	result, err := client.Pull(ctx, ref, destDir, klausoci.PersonalityArtifact)
	if err != nil {
		return nil, fmt.Errorf("pulling personality %s: %w", ref, err)
	}

	if result.Cached {
		fmt.Fprintf(w, "  %s: up-to-date (%s)\n", shortName, klausoci.TruncateDigest(result.Digest))
	} else {
		fmt.Fprintf(w, "  %s: pulled (%s)\n", shortName, klausoci.TruncateDigest(result.Digest))
	}

	spec, err := LoadPersonalitySpec(destDir)
	if err != nil {
		return nil, fmt.Errorf("loading personality spec: %w", err)
	}

	return &PersonalityResult{
		Spec:      spec,
		Dir:       destDir,
		ShortName: shortName,
	}, nil
}

// LoadPersonalitySpec reads and parses a personality.yaml from the given directory.
func LoadPersonalitySpec(dir string) (klausoci.PersonalitySpec, error) {
	data, err := os.ReadFile(filepath.Join(dir, "personality.yaml"))
	if err != nil {
		return klausoci.PersonalitySpec{}, fmt.Errorf("reading personality.yaml: %w", err)
	}

	var spec klausoci.PersonalitySpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return klausoci.PersonalitySpec{}, fmt.Errorf("parsing personality.yaml: %w", err)
	}

	return spec, nil
}

// HasSOULFile reports whether a pulled personality directory contains a SOUL.md.
func HasSOULFile(personalityDir string) bool {
	_, err := os.Stat(filepath.Join(personalityDir, "SOUL.md"))
	return err == nil
}

// PluginFromReference converts a klaus-oci PluginReference to a config.Plugin.
func PluginFromReference(ref klausoci.PluginReference) config.Plugin {
	return config.Plugin{
		Repository: ref.Repository,
		Tag:        ref.Tag,
		Digest:     ref.Digest,
	}
}

// MergePlugins merges personality plugins with user-configured plugins.
// User plugins take precedence: if a personality plugin and a user plugin
// share the same repository, the user's version is kept. Personality-only
// plugins are appended after user plugins.
func MergePlugins(personalityPlugins []klausoci.PluginReference, userPlugins []config.Plugin) []config.Plugin {
	if len(personalityPlugins) == 0 {
		return userPlugins
	}

	seen := make(map[string]bool, len(userPlugins))
	for _, p := range userPlugins {
		seen[p.Repository] = true
	}

	merged := make([]config.Plugin, len(userPlugins))
	copy(merged, userPlugins)

	for _, ref := range personalityPlugins {
		if seen[ref.Repository] {
			continue
		}
		seen[ref.Repository] = true
		merged = append(merged, PluginFromReference(ref))
	}

	return merged
}
