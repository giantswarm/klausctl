// Package server provides the MCP server context for klausctl.
package server

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/mcpclient"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

// ServerContext is a lightweight dependency container passed to MCP tool
// handlers. It provides access to klausctl paths, runtime detection, and
// the MCP client for agent communication.
type ServerContext struct {
	Paths     *config.Paths
	MCPClient *mcpclient.Client

	mu           sync.RWMutex
	sourceConfig *config.SourceConfig
}

// InstancePaths returns config paths scoped to a named instance.
func (sc *ServerContext) InstancePaths(name string) *config.Paths {
	return sc.Paths.ForInstance(name)
}

// LoadInstanceConfig loads the config for a named instance.
func (sc *ServerContext) LoadInstanceConfig(name string) (*config.Config, error) {
	paths := sc.InstancePaths(name)
	return config.Load(paths.ConfigFile)
}

// DetectRuntime creates a Runtime from the given config, auto-detecting
// when the config runtime field is empty.
func (sc *ServerContext) DetectRuntime(cfg *config.Config) (runtime.Runtime, error) {
	return runtime.New(cfg.Runtime)
}

// SetSourceConfig sets the loaded source configuration.
func (sc *ServerContext) SetSourceConfig(cfg *config.SourceConfig) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.sourceConfig = cfg
}

// ReloadSourceConfig re-reads the sources file from disk and updates the
// in-memory config. This should be called after mutating the sources file.
func (sc *ServerContext) ReloadSourceConfig() error {
	cfg, err := config.LoadSourceConfig(sc.Paths.SourcesFile)
	if err != nil {
		return err
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.sourceConfig = cfg
	return nil
}

// SourceConfig returns the current in-memory source configuration.
// If none has been loaded, a default config with only the built-in source
// is returned.
func (sc *ServerContext) SourceConfig() *config.SourceConfig {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	if sc.sourceConfig == nil {
		return config.DefaultSourceConfig()
	}
	return sc.sourceConfig
}

// SourceResolver returns a SourceResolver from the loaded source config.
// If no source config has been loaded, the default built-in source is used.
func (sc *ServerContext) SourceResolver() *config.SourceResolver {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	if sc.sourceConfig == nil {
		return config.DefaultSourceResolver()
	}
	return config.NewSourceResolver(sc.sourceConfig.Sources)
}

// JSONResult serializes v as indented JSON and returns it as an MCP text result.
func JSONResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshaling result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}
