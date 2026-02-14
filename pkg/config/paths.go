package config

import (
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
	// InstanceFile is the path to the instance state file.
	InstanceFile string
}

// DefaultPaths returns the default paths using XDG conventions.
func DefaultPaths() *Paths {
	configDir := filepath.Join(configHome(), "klausctl")
	return &Paths{
		ConfigDir:     configDir,
		ConfigFile:    filepath.Join(configDir, "config.yaml"),
		RenderedDir:   filepath.Join(configDir, "rendered"),
		ExtensionsDir: filepath.Join(configDir, "rendered", "extensions"),
		PluginsDir:    filepath.Join(configDir, "plugins"),
		InstanceFile:  filepath.Join(configDir, "instance.json"),
	}
}

// configHome returns the XDG config home directory.
func configHome() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "klausctl")
	}
	return filepath.Join(home, ".config")
}

// ExpandPath expands ~ to the user's home directory and resolves the path.
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
