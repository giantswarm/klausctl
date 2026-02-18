package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Paths holds the filesystem paths used by klausctl.
type Paths struct {
	// ConfigDir is the base config directory (~/.config/klausctl).
	ConfigDir string
	// ConfigFile is the path to the config file.
	ConfigFile string
	// InstancesDir is the directory containing all named instances.
	InstancesDir string
	// InstanceDir is the directory for the selected instance.
	InstanceDir string
	// RenderedDir is where rendered config files are written.
	RenderedDir string
	// ExtensionsDir is the rendered extensions directory (skills, agents).
	ExtensionsDir string
	// PluginsDir is where OCI plugins are stored.
	PluginsDir string
	// PersonalitiesDir is where OCI personalities are stored.
	PersonalitiesDir string
	// InstanceFile is the path to the instance state file.
	InstanceFile string
}

// DefaultPaths returns the default paths using XDG conventions.
// It returns an error if the user home directory cannot be determined
// and XDG_CONFIG_HOME is not set.
func DefaultPaths() (*Paths, error) {
	configDir, err := configHome()
	if err != nil {
		return nil, fmt.Errorf("determining config directory: %w", err)
	}
	base := filepath.Join(configDir, "klausctl")
	instancesDir := filepath.Join(base, "instances")
	defaultInstanceDir := filepath.Join(instancesDir, "default")
	return &Paths{
		ConfigDir:        base,
		ConfigFile:       filepath.Join(defaultInstanceDir, "config.yaml"),
		InstancesDir:     instancesDir,
		InstanceDir:      defaultInstanceDir,
		RenderedDir:      filepath.Join(defaultInstanceDir, "rendered"),
		ExtensionsDir:    filepath.Join(defaultInstanceDir, "rendered", "extensions"),
		PluginsDir:       filepath.Join(base, "plugins"),
		PersonalitiesDir: filepath.Join(base, "personalities"),
		InstanceFile:     filepath.Join(defaultInstanceDir, "instance.json"),
	}, nil
}

// configHome returns the XDG config home directory.
func configHome() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determining home directory: %w", err)
	}
	return filepath.Join(home, ".config"), nil
}

// ExpandPath expands ~ to the user's home directory and resolves the path.
// Note: only ~/... and bare ~ are supported; ~user syntax is not handled.
func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if path == "~" {
			return home
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// EnsureDir creates a directory and all parents if they don't exist.
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

// ForInstance returns a copy of paths scoped to one instance directory.
func (p *Paths) ForInstance(name string) *Paths {
	instanceName := strings.TrimSpace(name)
	if instanceName == "" {
		instanceName = "default"
	}

	instDir := filepath.Join(p.InstancesDir, instanceName)
	return &Paths{
		ConfigDir:        p.ConfigDir,
		ConfigFile:       filepath.Join(instDir, "config.yaml"),
		InstancesDir:     p.InstancesDir,
		InstanceDir:      instDir,
		RenderedDir:      filepath.Join(instDir, "rendered"),
		ExtensionsDir:    filepath.Join(instDir, "rendered", "extensions"),
		PluginsDir:       p.PluginsDir,
		PersonalitiesDir: p.PersonalitiesDir,
		InstanceFile:     filepath.Join(instDir, "instance.json"),
	}
}

var instanceNameRegexp = regexp.MustCompile(`^[a-zA-Z]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

// ValidateInstanceName validates a named instance using DNS-label rules.
func ValidateInstanceName(name string) error {
	if !instanceNameRegexp.MatchString(name) {
		return fmt.Errorf("invalid instance name %q: must start with a letter, contain only alphanumeric characters or '-', and be <= 63 characters", name)
	}
	return nil
}

const (
	// DefaultPluginRegistry is the default base reference for plugin short names.
	DefaultPluginRegistry = "gsoci.azurecr.io/giantswarm/klaus-plugins"
	// DefaultPersonalityRegistry is the default base reference for personality short names.
	DefaultPersonalityRegistry = "gsoci.azurecr.io/giantswarm/klaus-personalities"
	// DefaultToolchainRegistry is the default base reference for toolchain short names.
	DefaultToolchainRegistry = "gsoci.azurecr.io/giantswarm"
)

// ResolvePersonalityRef resolves full references and short names to OCI refs.
func ResolvePersonalityRef(ref string) string {
	return resolveArtifactRef(ref, DefaultPersonalityRegistry, "")
}

// ResolveToolchainRef resolves full references and short names to OCI refs.
func ResolveToolchainRef(ref string) string {
	return resolveArtifactRef(ref, DefaultToolchainRegistry, "klaus-")
}

// ResolvePluginRef resolves full references and short names to OCI refs.
func ResolvePluginRef(ref string) string {
	return resolveArtifactRef(ref, DefaultPluginRegistry, "")
}

func resolveArtifactRef(ref, base, namePrefix string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ref
	}

	// Full OCI refs keep their repository and only get :latest when no suffix exists.
	if strings.Contains(ref, "/") {
		if hasTagOrDigest(ref) {
			return ref
		}
		return ref + ":latest"
	}

	name, suffix := splitTagOrDigest(ref)
	if namePrefix != "" && !strings.HasPrefix(name, namePrefix) {
		name = namePrefix + name
	}
	if suffix == "" {
		suffix = ":latest"
	}
	return base + "/" + name + suffix
}

func hasTagOrDigest(ref string) bool {
	if strings.Contains(ref, "@") {
		return true
	}
	nameStart := strings.LastIndex(ref, "/")
	tagIdx := strings.LastIndex(ref, ":")
	return tagIdx > nameStart
}

func splitTagOrDigest(ref string) (string, string) {
	if idx := strings.Index(ref, "@"); idx >= 0 {
		return ref[:idx], ref[idx:]
	}
	nameStart := strings.LastIndex(ref, "/")
	tagIdx := strings.LastIndex(ref, ":")
	if tagIdx > nameStart {
		return ref[:tagIdx], ref[tagIdx:]
	}
	return ref, ""
}
