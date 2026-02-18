package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Paths holds the filesystem paths used by klausctl.
type Paths struct {
	// ConfigDir is the base config directory (~/.config/klausctl).
	ConfigDir string
	// ConfigFile is the path to the config file.
	ConfigFile string
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
	return &Paths{
		ConfigDir:        base,
		ConfigFile:       filepath.Join(base, "config.yaml"),
		RenderedDir:      filepath.Join(base, "rendered"),
		ExtensionsDir:    filepath.Join(base, "rendered", "extensions"),
		PluginsDir:       filepath.Join(base, "plugins"),
		PersonalitiesDir: filepath.Join(base, "personalities"),
		InstanceFile:     filepath.Join(base, "instance.json"),
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
