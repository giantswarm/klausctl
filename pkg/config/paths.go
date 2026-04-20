package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	ws "github.com/giantswarm/klausctl/pkg/workspace"
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
	// ArchivesDir is the directory for archived instance transcripts.
	ArchivesDir string
	// SecretsFile is the path to the secrets store (~/.config/klausctl/secrets.yaml).
	SecretsFile string
	// TokensDir is the directory for stored OAuth tokens (~/.config/klausctl/tokens/).
	TokensDir string
	// McpServersFile is the path to the managed MCP servers file (~/.config/klausctl/mcpservers.yaml).
	McpServersFile string
	// SourcesFile is the path to the sources configuration file (~/.config/klausctl/sources.yaml).
	SourcesFile string
	// MusterConfigDir is the muster config root (~/.config/klausctl/muster/).
	// Contains muster's own config.yaml and the mcpservers/ subdirectory.
	MusterConfigDir string
	// MusterMCPServersDir is where muster-native MCPServerSpec YAML files live
	// (~/.config/klausctl/muster/mcpservers/).
	MusterMCPServersDir string
	// MusterPIDFile tracks the PID of the managed muster process.
	MusterPIDFile string
	// MusterPortFile tracks the port of the managed muster process.
	MusterPortFile string
	// GatewayConfigDir is the user-owned klaus-gateway config root
	// (~/.config/klausctl/gateway/). Contains config.yaml, routes.bolt, and
	// slack-secrets.yaml.
	GatewayConfigDir string
	// GatewayConfigFile is the user-owned gateway/config.yaml
	// (~/.config/klausctl/gateway/config.yaml).
	GatewayConfigFile string
	// GatewayRoutesBoltFile is the klausctl-owned routes store passed to
	// klaus-gateway as --bolt-path (~/.config/klausctl/gateway/routes.bolt).
	GatewayRoutesBoltFile string
	// GatewaySlackSecretsFile is the user-owned slack adapter secrets file
	// (~/.config/klausctl/gateway/slack-secrets.yaml).
	GatewaySlackSecretsFile string
	// KlausGatewayPIDFile tracks the PID of the managed klaus-gateway process.
	KlausGatewayPIDFile string
	// KlausGatewayPortFile tracks the port of the managed klaus-gateway process.
	KlausGatewayPortFile string
	// AgentGatewayPIDFile tracks the PID of the managed agentgateway process.
	AgentGatewayPIDFile string
	// AgentGatewayPortFile tracks the port of the managed agentgateway process.
	AgentGatewayPortFile string
	// ReposDir is the managed repo cache directory (~/.config/klausctl/repos/).
	ReposDir string
	// WorkspacesFile is the path to the workspace registry (~/.config/klausctl/workspaces.yaml).
	WorkspacesFile string
	// AuthDir is the directory for host-keyed remote-gateway OAuth records
	// (~/.config/klausctl/auth/), used by `klausctl auth login --remote=URL`.
	AuthDir string
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

	sourcesFile := filepath.Join(base, "sources.yaml")
	if override := os.Getenv("KLAUSCTL_SOURCES_FILE"); override != "" {
		sourcesFile = filepath.Clean(override)
	}

	musterDir := filepath.Join(base, "muster")
	gatewayDir := filepath.Join(base, "gateway")

	return &Paths{
		ConfigDir:               base,
		ConfigFile:              filepath.Join(defaultInstanceDir, "config.yaml"),
		InstancesDir:            instancesDir,
		InstanceDir:             defaultInstanceDir,
		RenderedDir:             filepath.Join(defaultInstanceDir, "rendered"),
		ExtensionsDir:           filepath.Join(defaultInstanceDir, "rendered", "extensions"),
		PluginsDir:              filepath.Join(base, "plugins"),
		PersonalitiesDir:        filepath.Join(base, "personalities"),
		InstanceFile:            filepath.Join(defaultInstanceDir, "instance.json"),
		ArchivesDir:             filepath.Join(base, "archives"),
		TokensDir:               filepath.Join(base, "tokens"),
		SecretsFile:             filepath.Join(base, "secrets.yaml"),
		McpServersFile:          filepath.Join(base, "mcpservers.yaml"),
		SourcesFile:             sourcesFile,
		MusterConfigDir:         musterDir,
		MusterMCPServersDir:     filepath.Join(musterDir, "mcpservers"),
		MusterPIDFile:           filepath.Join(base, "muster.pid"),
		MusterPortFile:          filepath.Join(base, "muster.port"),
		GatewayConfigDir:        gatewayDir,
		GatewayConfigFile:       filepath.Join(gatewayDir, "config.yaml"),
		GatewayRoutesBoltFile:   filepath.Join(gatewayDir, "routes.bolt"),
		GatewaySlackSecretsFile: filepath.Join(gatewayDir, "slack-secrets.yaml"),
		KlausGatewayPIDFile:     filepath.Join(base, "klaus-gateway.pid"),
		KlausGatewayPortFile:    filepath.Join(base, "klaus-gateway.port"),
		AgentGatewayPIDFile:     filepath.Join(base, "agentgateway.pid"),
		AgentGatewayPortFile:    filepath.Join(base, "agentgateway.port"),
		ReposDir:                filepath.Join(base, "repos"),
		WorkspacesFile:          filepath.Join(base, "workspaces.yaml"),
		AuthDir:                 filepath.Join(base, "auth"),
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

// ResolveWorkspacePath resolves a workspace string to an absolute host path.
// Repo identifiers (owner/repo) are resolved under reposDir; filesystem
// paths are tilde-expanded via ExpandPath.
func ResolveWorkspacePath(workspace, reposDir string) string {
	if ws.IsRepoIdentifier(workspace) {
		return filepath.Join(reposDir, workspace)
	}
	return ExpandPath(workspace)
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
		ConfigDir:               p.ConfigDir,
		ConfigFile:              filepath.Join(instDir, "config.yaml"),
		InstancesDir:            p.InstancesDir,
		InstanceDir:             instDir,
		RenderedDir:             filepath.Join(instDir, "rendered"),
		ExtensionsDir:           filepath.Join(instDir, "rendered", "extensions"),
		PluginsDir:              p.PluginsDir,
		PersonalitiesDir:        p.PersonalitiesDir,
		InstanceFile:            filepath.Join(instDir, "instance.json"),
		ArchivesDir:             p.ArchivesDir,
		TokensDir:               p.TokensDir,
		SecretsFile:             p.SecretsFile,
		McpServersFile:          p.McpServersFile,
		SourcesFile:             p.SourcesFile,
		MusterConfigDir:         p.MusterConfigDir,
		MusterMCPServersDir:     p.MusterMCPServersDir,
		MusterPIDFile:           p.MusterPIDFile,
		MusterPortFile:          p.MusterPortFile,
		GatewayConfigDir:        p.GatewayConfigDir,
		GatewayConfigFile:       p.GatewayConfigFile,
		GatewayRoutesBoltFile:   p.GatewayRoutesBoltFile,
		GatewaySlackSecretsFile: p.GatewaySlackSecretsFile,
		KlausGatewayPIDFile:     p.KlausGatewayPIDFile,
		KlausGatewayPortFile:    p.KlausGatewayPortFile,
		AgentGatewayPIDFile:     p.AgentGatewayPIDFile,
		AgentGatewayPortFile:    p.AgentGatewayPortFile,
		ReposDir:                p.ReposDir,
		WorkspacesFile:          p.WorkspacesFile,
		AuthDir:                 p.AuthDir,
	}
}

// HasMusterConfig reports whether the muster config directory contains at
// least one MCP server YAML file. The bridge should only start when there is
// something to serve.
func (p *Paths) HasMusterConfig() (bool, error) {
	entries, err := os.ReadDir(p.MusterMCPServersDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("reading muster mcpservers directory: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) == ".yaml" || filepath.Ext(name) == ".yml" {
			return true, nil
		}
	}
	return false, nil
}

var instanceNameRegexp = regexp.MustCompile(`^[a-zA-Z]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

// ValidateInstanceName validates a named instance using DNS-label rules.
func ValidateInstanceName(name string) error {
	if !instanceNameRegexp.MatchString(name) {
		return fmt.Errorf("invalid instance name %q: must start with a letter, contain only alphanumeric characters or '-', and be <= 63 characters", name)
	}
	return nil
}

// expandArtifactRef expands short names (no "/") into fully-qualified
// repository paths. Full OCI refs and any existing tag/digest suffix are
// kept as-is. Unlike oci.ResolveArtifactRef this is offline and never
// appends ":latest" -- tag resolution is deferred to start time.
func expandArtifactRef(ref, base string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ref
	}

	if strings.Contains(ref, "/") {
		return ref
	}

	name, suffix := splitNameSuffix(ref)
	return base + "/" + name + suffix
}

func splitNameSuffix(ref string) (string, string) {
	if idx := strings.Index(ref, "@"); idx >= 0 {
		return ref[:idx], ref[idx:]
	}
	if idx := strings.LastIndex(ref, ":"); idx >= 0 {
		return ref[:idx], ref[idx:]
	}
	return ref, ""
}
