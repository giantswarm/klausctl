package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultSourceName is the built-in source that cannot be removed.
	DefaultSourceName = "giantswarm"
	// DefaultSourceRegistry is the registry base for the built-in source.
	DefaultSourceRegistry = "gsoci.azurecr.io/giantswarm"
)

// Source is a named OCI registry providing toolchains, personalities, and/or plugins.
type Source struct {
	Name          string `yaml:"name"`
	Registry      string `yaml:"registry"`
	Default       bool   `yaml:"default,omitempty"`
	Toolchains    string `yaml:"toolchains,omitempty"`
	Personalities string `yaml:"personalities,omitempty"`
	Plugins       string `yaml:"plugins,omitempty"`
}

// ToolchainRegistry returns the toolchain base path for this source.
// Falls back to convention: <registry>/klaus-toolchains
func (s Source) ToolchainRegistry() string {
	if s.Toolchains != "" {
		return s.Toolchains
	}
	return s.Registry + "/klaus-toolchains"
}

// PersonalityRegistry returns the personality base path for this source.
// Falls back to convention: <registry>/klaus-personalities
func (s Source) PersonalityRegistry() string {
	if s.Personalities != "" {
		return s.Personalities
	}
	return s.Registry + "/klaus-personalities"
}

// PluginRegistry returns the plugin base path for this source.
// Falls back to convention: <registry>/klaus-plugins
func (s Source) PluginRegistry() string {
	if s.Plugins != "" {
		return s.Plugins
	}
	return s.Registry + "/klaus-plugins"
}

// SourceConfig holds the list of configured sources.
type SourceConfig struct {
	Sources []Source `yaml:"sources"`
	path    string
}

// SourceRegistry pairs a source name with a registry base path.
type SourceRegistry struct {
	Source   string
	Registry string
}

var sourceNameRegexp = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]{0,62}$`)

// ValidateSourceName checks that a source name is valid.
func ValidateSourceName(name string) error {
	if !sourceNameRegexp.MatchString(name) {
		return fmt.Errorf("invalid source name %q: must start with a letter, contain only alphanumeric characters or hyphens (underscores are not allowed), and be 1-63 characters", name)
	}
	return nil
}

// builtinSource returns the default built-in Giant Swarm source.
func builtinSource() Source {
	return Source{
		Name:     DefaultSourceName,
		Registry: DefaultSourceRegistry,
		Default:  true,
	}
}

// DefaultSourceConfig returns a source config with only the built-in source.
func DefaultSourceConfig() *SourceConfig {
	return &SourceConfig{
		Sources: []Source{builtinSource()},
	}
}

// LoadSourceConfig reads and parses the sources configuration file.
// If the file does not exist, the default config (built-in source only) is returned.
func LoadSourceConfig(path string) (*SourceConfig, error) {
	sc := &SourceConfig{path: path}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			sc.Sources = []Source{builtinSource()}
			return sc, nil
		}
		return nil, fmt.Errorf("reading sources config: %w", err)
	}

	if err := yaml.Unmarshal(data, sc); err != nil {
		return nil, fmt.Errorf("parsing sources config: %w", err)
	}

	sc.ensureBuiltin()

	if err := sc.Validate(); err != nil {
		return nil, fmt.Errorf("invalid sources config: %w", err)
	}

	return sc, nil
}

// Save writes the source config to disk.
func (sc *SourceConfig) Save() error {
	if sc.path == "" {
		return fmt.Errorf("source config path not set")
	}
	return sc.SaveTo(sc.path)
}

// SaveTo writes the source config to the specified path.
func (sc *SourceConfig) SaveTo(path string) error {
	data, err := yaml.Marshal(sc)
	if err != nil {
		return fmt.Errorf("serializing sources config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing sources config: %w", err)
	}
	sc.path = path
	return nil
}

// Validate checks the source configuration for errors.
func (sc *SourceConfig) Validate() error {
	seen := make(map[string]bool, len(sc.Sources))
	defaultCount := 0
	for _, s := range sc.Sources {
		if err := ValidateSourceName(s.Name); err != nil {
			return err
		}
		if s.Registry == "" {
			return fmt.Errorf("source %q: registry is required", s.Name)
		}
		if seen[s.Name] {
			return fmt.Errorf("duplicate source name %q", s.Name)
		}
		seen[s.Name] = true
		if s.Default {
			defaultCount++
		}
	}
	if defaultCount > 1 {
		return fmt.Errorf("multiple sources marked as default; only one is allowed")
	}
	return nil
}

// ensureBuiltin ensures the built-in Giant Swarm source is always present.
// If no other source is marked as default, the builtin gets Default: true.
func (sc *SourceConfig) ensureBuiltin() {
	for _, s := range sc.Sources {
		if s.Name == DefaultSourceName {
			return
		}
	}
	hasDefault := false
	for _, s := range sc.Sources {
		if s.Default {
			hasDefault = true
			break
		}
	}
	b := builtinSource()
	if hasDefault {
		b.Default = false
	}
	sc.Sources = append([]Source{b}, sc.Sources...)
}

// Add adds a new source. Returns an error if a source with the same name already exists.
func (sc *SourceConfig) Add(s Source) error {
	for _, existing := range sc.Sources {
		if existing.Name == s.Name {
			return fmt.Errorf("source %q already exists", s.Name)
		}
	}
	if err := ValidateSourceName(s.Name); err != nil {
		return err
	}
	if s.Registry == "" {
		return fmt.Errorf("registry is required")
	}
	sc.Sources = append(sc.Sources, s)
	return nil
}

// Remove removes a source by name. The built-in source cannot be removed.
func (sc *SourceConfig) Remove(name string) error {
	if name == DefaultSourceName {
		return fmt.Errorf("cannot remove built-in source %q", DefaultSourceName)
	}
	for i, s := range sc.Sources {
		if s.Name == name {
			sc.Sources = append(sc.Sources[:i], sc.Sources[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("source %q not found", name)
}

// SetDefault marks the named source as default (and clears default on all others).
func (sc *SourceConfig) SetDefault(name string) error {
	found := false
	for i := range sc.Sources {
		if sc.Sources[i].Name == name {
			sc.Sources[i].Default = true
			found = true
		} else {
			sc.Sources[i].Default = false
		}
	}
	if !found {
		return fmt.Errorf("source %q not found", name)
	}
	return nil
}

// Get returns the source with the given name, or nil if not found.
func (sc *SourceConfig) Get(name string) *Source {
	for i := range sc.Sources {
		if sc.Sources[i].Name == name {
			return &sc.Sources[i]
		}
	}
	return nil
}

// Update modifies an existing source. Only non-empty fields in the
// provided Source are applied (registry, toolchains, personalities, plugins).
// Returns an error if the source is not found.
func (sc *SourceConfig) Update(name string, patch Source) error {
	for i := range sc.Sources {
		if sc.Sources[i].Name != name {
			continue
		}
		if patch.Registry != "" {
			sc.Sources[i].Registry = patch.Registry
		}
		if patch.Toolchains != "" {
			sc.Sources[i].Toolchains = patch.Toolchains
		}
		if patch.Personalities != "" {
			sc.Sources[i].Personalities = patch.Personalities
		}
		if patch.Plugins != "" {
			sc.Sources[i].Plugins = patch.Plugins
		}
		return nil
	}
	return fmt.Errorf("source %q not found", name)
}

// SourceResolver wraps a list of sources and provides artifact reference resolution.
// The default source (if any) is placed first for short-name resolution priority.
type SourceResolver struct {
	sources []Source
}

// NewSourceResolver creates a resolver from the given sources.
// If sources is empty, the built-in default source is used.
// Sources are reordered so the one marked Default comes first.
func NewSourceResolver(sources []Source) *SourceResolver {
	if len(sources) == 0 {
		sources = []Source{builtinSource()}
	}
	ordered := make([]Source, len(sources))
	copy(ordered, sources)
	slices.SortStableFunc(ordered, func(a, b Source) int {
		if a.Default && !b.Default {
			return -1
		}
		if !a.Default && b.Default {
			return 1
		}
		return 0
	})
	return &SourceResolver{sources: ordered}
}

// DefaultSourceResolver returns a resolver with only the built-in source.
func DefaultSourceResolver() *SourceResolver {
	return NewSourceResolver(nil)
}

// ForSource returns a resolver restricted to a single named source.
// Returns an error if the source is not found.
func (r *SourceResolver) ForSource(name string) (*SourceResolver, error) {
	for _, s := range r.sources {
		if s.Name == name {
			return NewSourceResolver([]Source{s}), nil
		}
	}
	return nil, fmt.Errorf("source %q not found", name)
}

// DefaultOnly returns a resolver restricted to the default source (the
// first source after default-first ordering).
func (r *SourceResolver) DefaultOnly() *SourceResolver {
	return NewSourceResolver([]Source{r.sources[0]})
}

// ResolvePluginRef expands a short plugin name using the default source.
func (r *SourceResolver) ResolvePluginRef(ref string) string {
	return expandArtifactRef(ref, r.sources[0].PluginRegistry())
}

// ResolvePersonalityRef expands a short personality name using the default source.
func (r *SourceResolver) ResolvePersonalityRef(ref string) string {
	return expandArtifactRef(ref, r.sources[0].PersonalityRegistry())
}

// ResolveToolchainRef expands a short toolchain name using the default source.
func (r *SourceResolver) ResolveToolchainRef(ref string) string {
	return expandArtifactRef(ref, r.sources[0].ToolchainRegistry())
}

// PluginRegistries returns all plugin registry bases with source annotations.
func (r *SourceResolver) PluginRegistries() []SourceRegistry {
	result := make([]SourceRegistry, len(r.sources))
	for i, s := range r.sources {
		result[i] = SourceRegistry{Source: s.Name, Registry: s.PluginRegistry()}
	}
	return result
}

// PersonalityRegistries returns all personality registry bases with source annotations.
func (r *SourceResolver) PersonalityRegistries() []SourceRegistry {
	result := make([]SourceRegistry, len(r.sources))
	for i, s := range r.sources {
		result[i] = SourceRegistry{Source: s.Name, Registry: s.PersonalityRegistry()}
	}
	return result
}

// ToolchainRegistries returns all toolchain registry bases with source annotations.
func (r *SourceResolver) ToolchainRegistries() []SourceRegistry {
	result := make([]SourceRegistry, len(r.sources))
	for i, s := range r.sources {
		result[i] = SourceRegistry{Source: s.Name, Registry: s.ToolchainRegistry()}
	}
	return result
}

// Sources returns a copy of the underlying list of sources.
func (r *SourceResolver) Sources() []Source {
	return slices.Clone(r.sources)
}
