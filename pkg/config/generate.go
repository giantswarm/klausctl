package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

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

	// Override fields applied after personality resolution.
	EnvVars        map[string]string
	EnvForward     []string
	McpServers     map[string]any
	SecretEnvVars  map[string]string
	SecretFiles    map[string]string
	McpServerRefs  []string
	MaxBudgetUSD   *float64
	PermissionMode string
	Model          string
	SystemPrompt   string

	// SourceResolver provides multi-source artifact resolution.
	// When nil, the default built-in source is used.
	SourceResolver *SourceResolver

	// Context and Output are passed to ResolvePersonality when provided.
	Context context.Context
	Output  io.Writer

	// ResolvePersonality optionally resolves and pulls a personality reference.
	// Keeping this as a callback avoids package cycles while allowing
	// GenerateInstanceConfig to encapsulate create-time merge behavior.
	ResolvePersonality func(ctx context.Context, ref string, w io.Writer) (*ResolvedPersonality, error)
}

// ResolvedPersonality contains personality-derived values merged into config.
type ResolvedPersonality struct {
	Plugins []Plugin
	Image   string
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

	resolver := opts.SourceResolver
	if resolver == nil {
		resolver = DefaultSourceResolver()
	}

	toolchainExplicitlySet := opts.Toolchain != ""
	if opts.Personality != "" {
		cfg.Personality = resolver.ResolvePersonalityRef(opts.Personality)
	}

	if toolchainExplicitlySet {
		cfg.Toolchain = resolver.ResolveToolchainRef(opts.Toolchain)
		cfg.Image = cfg.Toolchain
	}

	for _, pluginRef := range opts.Plugins {
		cfg.Plugins = append(cfg.Plugins, ParsePluginRefWith(pluginRef, resolver))
	}

	if opts.Port > 0 {
		used, err := UsedPorts(paths)
		if err != nil {
			return nil, err
		}
		if used[opts.Port] {
			return nil, fmt.Errorf("port %d is already used by another instance; choose a different --port or omit --port for auto-selection", opts.Port)
		}
		cfg.Port = opts.Port
	} else {
		port, err := NextAvailablePort(paths, 8080)
		if err != nil {
			return nil, err
		}
		cfg.Port = port
	}

	if cfg.Personality != "" && opts.ResolvePersonality != nil {
		ctx := opts.Context
		if ctx == nil {
			ctx = context.Background()
		}

		resolved, err := opts.ResolvePersonality(ctx, cfg.Personality, opts.Output)
		if err != nil {
			return nil, fmt.Errorf("resolving personality: %w", err)
		}

		cfg.Plugins = mergePlugins(resolved.Plugins, cfg.Plugins)
		if !toolchainExplicitlySet && resolved.Image != "" {
			cfg.Image = resolved.Image
		}
	}

	applyCreateOverrides(cfg, opts)

	return cfg, cfg.Validate()
}

// applyCreateOverrides merges optional override fields from CreateOptions into
// the generated config. Called after personality resolution, before validation.
func applyCreateOverrides(cfg *Config, opts CreateOptions) {
	for k, v := range opts.EnvVars {
		if cfg.EnvVars == nil {
			cfg.EnvVars = make(map[string]string, len(opts.EnvVars))
		}
		cfg.EnvVars[k] = v
	}

	if len(opts.EnvForward) > 0 {
		cfg.EnvForward = append(cfg.EnvForward, opts.EnvForward...)
		slices.Sort(cfg.EnvForward)
		cfg.EnvForward = slices.Compact(cfg.EnvForward)
	}

	for k, v := range opts.McpServers {
		if cfg.McpServers == nil {
			cfg.McpServers = make(map[string]any, len(opts.McpServers))
		}
		cfg.McpServers[k] = v
	}

	for k, v := range opts.SecretEnvVars {
		if cfg.SecretEnvVars == nil {
			cfg.SecretEnvVars = make(map[string]string, len(opts.SecretEnvVars))
		}
		cfg.SecretEnvVars[k] = v
	}

	for k, v := range opts.SecretFiles {
		if cfg.SecretFiles == nil {
			cfg.SecretFiles = make(map[string]string, len(opts.SecretFiles))
		}
		cfg.SecretFiles[k] = v
	}

	if len(opts.McpServerRefs) > 0 {
		cfg.McpServerRefs = append(cfg.McpServerRefs, opts.McpServerRefs...)
		slices.Sort(cfg.McpServerRefs)
		cfg.McpServerRefs = slices.Compact(cfg.McpServerRefs)
	}

	if opts.MaxBudgetUSD != nil {
		cfg.Claude.MaxBudgetUSD = *opts.MaxBudgetUSD
	}
	if opts.PermissionMode != "" {
		cfg.Claude.PermissionMode = opts.PermissionMode
	}
	if opts.Model != "" {
		cfg.Claude.Model = opts.Model
	}
	if opts.SystemPrompt != "" {
		cfg.Claude.SystemPrompt = opts.SystemPrompt
	}
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

// ParsePluginRef resolves a plugin reference into config.Plugin fields
// using the default built-in source.
func ParsePluginRef(ref string) Plugin {
	return ParsePluginRefWith(ref, DefaultSourceResolver())
}

// ParsePluginRefWith resolves a plugin reference into config.Plugin fields
// using the provided SourceResolver.
func ParsePluginRefWith(ref string, resolver *SourceResolver) Plugin {
	resolved := resolver.ResolvePluginRef(ref)
	repository, suffix := splitNameSuffix(resolved)

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

func mergePlugins(personalityPlugins, userPlugins []Plugin) []Plugin {
	if len(personalityPlugins) == 0 {
		return userPlugins
	}

	seen := make(map[string]bool, len(userPlugins))
	for _, p := range userPlugins {
		seen[p.Repository] = true
	}

	merged := make([]Plugin, len(userPlugins))
	copy(merged, userPlugins)

	for _, p := range personalityPlugins {
		if seen[p.Repository] {
			continue
		}
		seen[p.Repository] = true
		merged = append(merged, p)
	}

	return merged
}
