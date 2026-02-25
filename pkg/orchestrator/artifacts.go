package orchestrator

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	klausoci "github.com/giantswarm/klaus-oci"

	"github.com/giantswarm/klausctl/pkg/config"
)

const registryAuthEnvVar = "KLAUSCTL_REGISTRY_AUTH"

// NewDefaultClient creates an OCI client configured with the standard
// klausctl credential resolution: Docker/Podman config files plus the
// KLAUSCTL_REGISTRY_AUTH environment variable.
func NewDefaultClient(opts ...klausoci.ClientOption) *klausoci.Client {
	return klausoci.NewClient(append([]klausoci.ClientOption{klausoci.WithRegistryAuthEnv(registryAuthEnvVar)}, opts...)...)
}

// ResolveCreateRefs resolves personality, toolchain, and plugin short names
// to full OCI references with proper semver tags from the registry.
// The resolver is used to expand short names against configured sources;
// if nil, the default built-in source is used.
func ResolveCreateRefs(ctx context.Context, resolver *config.SourceResolver, personality, toolchain string, plugins []string) (string, string, []string, error) {
	if resolver == nil {
		resolver = config.DefaultSourceResolver()
	}
	client := NewDefaultClient()

	if personality != "" {
		expanded := resolver.ResolvePersonalityRef(personality)
		ref, err := client.ResolvePersonalityRef(ctx, expanded)
		if err != nil {
			return "", "", nil, fmt.Errorf("resolving personality: %w", err)
		}
		personality = ref
	}

	if toolchain != "" {
		expanded := resolver.ResolveToolchainRef(toolchain)
		ref, err := client.ResolveToolchainRef(ctx, expanded)
		if err != nil {
			return "", "", nil, fmt.Errorf("resolving toolchain: %w", err)
		}
		toolchain = ref
	}

	resolved := make([]string, 0, len(plugins))
	for _, p := range plugins {
		expanded := resolver.ResolvePluginRef(p)
		ref, err := client.ResolvePluginRef(ctx, expanded)
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
func ResolvePluginRefs(ctx context.Context, client *klausoci.Client, refs []klausoci.PluginReference) ([]config.Plugin, error) {
	plugins := make([]config.Plugin, 0, len(refs))
	for _, ref := range refs {
		resolved, err := client.ResolvePluginRef(ctx, ref.Ref())
		if err != nil {
			return nil, fmt.Errorf("resolving plugin %s: %w", ref.Ref(), err)
		}
		repo, tag := klausoci.SplitNameTag(resolved)
		plugins = append(plugins, config.Plugin{
			Repository: repo,
			Tag:        tag,
		})
	}
	return plugins, nil
}

// PullPlugins pulls all configured plugins to the local plugins directory.
// Each plugin is stored at <pluginsDir>/<shortName>/. Plugins are cached by
// digest and skipped if already up-to-date. Progress messages are written to w.
//
// Plugins with a "latest" tag or no tag are resolved to the latest semver
// tag from the registry before pulling.
func PullPlugins(ctx context.Context, client *klausoci.Client, plugins []config.Plugin, pluginsDir string, w io.Writer) error {
	for _, p := range plugins {
		ref := BuildRef(p)

		resolved, err := client.ResolvePluginRef(ctx, ref)
		if err != nil {
			return fmt.Errorf("resolving plugin %s: %w", ref, err)
		}

		shortName := klausoci.ShortName(klausoci.RepositoryFromRef(resolved))
		destDir := filepath.Join(pluginsDir, shortName)

		fmt.Fprintf(w, "  Pulling %s...\n", resolved)

		result, err := client.PullPlugin(ctx, resolved, destDir)
		if err != nil {
			return fmt.Errorf("pulling plugin %s: %w", resolved, err)
		}

		if result.Cached {
			fmt.Fprintf(w, "  %s: up-to-date (%s)\n", shortName, klausoci.TruncateDigest(result.Digest))
		} else {
			fmt.Fprintf(w, "  %s: pulled (%s)\n", shortName, klausoci.TruncateDigest(result.Digest))
		}
	}

	return nil
}

// PluginDirs returns the container-internal mount paths for the given plugins.
// Each plugin is mounted at /var/lib/klaus/plugins/<shortName>.
func PluginDirs(plugins []config.Plugin) []string {
	dirs := make([]string, 0, len(plugins))
	for _, p := range plugins {
		dirs = append(dirs, "/var/lib/klaus/plugins/"+klausoci.ShortName(p.Repository))
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
	// Spec is the parsed personality metadata.
	Spec klausoci.Personality
	// Dir is the local directory where the personality was pulled.
	Dir string
	// ShortName is the short name extracted from the OCI reference.
	ShortName string
}

// ResolvePersonality pulls a personality OCI artifact and parses its spec.
// The personality is stored at <personalitiesDir>/<shortName>/.
func ResolvePersonality(ctx context.Context, client *klausoci.Client, ref, personalitiesDir string, w io.Writer) (*PersonalityResult, error) {
	repo := klausoci.RepositoryFromRef(ref)
	shortName := klausoci.ShortName(repo)
	destDir := filepath.Join(personalitiesDir, shortName)

	fmt.Fprintf(w, "  Pulling personality %s...\n", ref)
	result, err := client.PullPersonality(ctx, ref, destDir)
	if err != nil {
		return nil, fmt.Errorf("pulling personality %s: %w", ref, err)
	}

	if result.Cached {
		fmt.Fprintf(w, "  %s: up-to-date (%s)\n", shortName, klausoci.TruncateDigest(result.Digest))
	} else {
		fmt.Fprintf(w, "  %s: pulled (%s)\n", shortName, klausoci.TruncateDigest(result.Digest))
	}

	return &PersonalityResult{
		Spec:      result.Personality,
		Dir:       destDir,
		ShortName: shortName,
	}, nil
}

// LoadPersonalitySpec reads and parses a personality.yaml from the given directory.
func LoadPersonalitySpec(dir string) (klausoci.Personality, error) {
	p, err := klausoci.ReadPersonalityFromDir(dir)
	if err != nil {
		return klausoci.Personality{}, err
	}
	return *p, nil
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
