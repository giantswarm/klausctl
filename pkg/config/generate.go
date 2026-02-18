package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// CreateOptions defines user-facing parameters for klausctl create.
type CreateOptions struct {
	Name        string
	Workspace   string
	Personality string
	Toolchain   string
	Plugins     []string
	Port        int
}

// GenerateInstanceConfig builds a per-instance configuration from create options.
func GenerateInstanceConfig(paths *Paths, opts CreateOptions) (*Config, error) {
	if err := ValidateInstanceName(opts.Name); err != nil {
		return nil, err
	}

	workspace := ExpandPath(opts.Workspace)
	info, err := os.Stat(workspace)
	if err != nil {
		return nil, fmt.Errorf("checking workspace directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace path is not a directory: %s", workspace)
	}

	cfg := DefaultConfig()
	cfg.Workspace = workspace

	if opts.Personality != "" {
		cfg.Personality = ResolvePersonalityRef(opts.Personality)
	}

	if opts.Toolchain != "" {
		cfg.Image = ResolveToolchainRef(opts.Toolchain)
	}

	for _, pluginRef := range opts.Plugins {
		cfg.Plugins = append(cfg.Plugins, ParsePluginRef(pluginRef))
	}

	if opts.Port > 0 {
		cfg.Port = opts.Port
	} else {
		port, err := NextAvailablePort(paths, 8080)
		if err != nil {
			return nil, err
		}
		cfg.Port = port
	}

	return cfg, cfg.Validate()
}

// NextAvailablePort returns the lowest free port >= start.
func NextAvailablePort(paths *Paths, start int) (int, error) {
	used, err := UsedPorts(paths)
	if err != nil {
		return 0, err
	}
	for p := start; p <= 65535; p++ {
		if !used[p] {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no available ports in range %d-65535", start)
}

// UsedPorts returns ports currently known in config or instance state files.
func UsedPorts(paths *Paths) (map[int]bool, error) {
	used := make(map[int]bool)

	entries, err := os.ReadDir(paths.InstancesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return used, nil
		}
		return nil, fmt.Errorf("reading instances directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		instDir := filepath.Join(paths.InstancesDir, entry.Name())
		stateFile := filepath.Join(instDir, "instance.json")
		configFile := filepath.Join(instDir, "config.yaml")

		var st struct {
			Port int `json:"port"`
		}
		if data, err := os.ReadFile(stateFile); err == nil {
			if err := json.Unmarshal(data, &st); err == nil && st.Port > 0 {
				used[st.Port] = true
			}
		}

		var cfg struct {
			Port int `yaml:"port"`
		}
		if data, err := os.ReadFile(configFile); err == nil {
			if err := yaml.Unmarshal(data, &cfg); err == nil && cfg.Port > 0 {
				used[cfg.Port] = true
			}
		}
	}

	return used, nil
}

// ParsePluginRef resolves a plugin reference into config.Plugin fields.
func ParsePluginRef(ref string) Plugin {
	resolved := ResolvePluginRef(ref)
	repository, suffix := splitTagOrDigest(resolved)

	plugin := Plugin{Repository: repository}
	if len(suffix) > 0 {
		if suffix[0] == ':' {
			plugin.Tag = suffix[1:]
		}
		if suffix[0] == '@' {
			plugin.Digest = suffix[1:]
		}
	}

	return plugin
}
