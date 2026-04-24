package gatewaybridge

import (
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// gatewayConfig is the subset of gateway/config.yaml that klausctl reads.
// The file is user-owned: klausctl never writes to it.
type gatewayConfig struct {
	// LogLevel configures the --log-level flag passed to klaus-gateway.
	LogLevel string `yaml:"logLevel,omitempty"`
	// Port overrides the klaus-gateway listen port.
	Port int `yaml:"port,omitempty"`
	// Adapters toggles per-channel adapters. Keys are adapter names (e.g.
	// "slack"), values are a small config struct. The set of enabled
	// adapters is surfaced in the Status payload.
	Adapters map[string]adapterConfig `yaml:"adapters,omitempty"`
	// AgentGateway configures the optional upstream wire-format proxy.
	AgentGateway agentGatewayConfig `yaml:"agentGateway,omitempty"`
}

type adapterConfig struct {
	Enabled bool `yaml:"enabled,omitempty"`
}

type agentGatewayConfig struct {
	Enabled bool `yaml:"enabled,omitempty"`
	Port    int  `yaml:"port,omitempty"`
}

// loadGatewayConfig reads and parses the gateway/config.yaml file. A missing
// or unreadable file yields a zero config (all defaults).
func loadGatewayConfig(path string) gatewayConfig {
	data, err := os.ReadFile(path) // #nosec G304 -- user-supplied or trusted local path; not exposed to untrusted input
	if err != nil {
		return gatewayConfig{}
	}
	var gc gatewayConfig
	if err := yaml.Unmarshal(data, &gc); err != nil {
		return gatewayConfig{}
	}
	return gc
}

// EnabledAdapters returns the sorted names of adapters with `enabled: true`
// in gateway/config.yaml.
func (c gatewayConfig) EnabledAdapters() []string {
	if len(c.Adapters) == 0 {
		return nil
	}
	names := make([]string, 0, len(c.Adapters))
	for name, ac := range c.Adapters {
		if ac.Enabled {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
