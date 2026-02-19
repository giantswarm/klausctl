// Package server provides the MCP server context for klausctl.
package server

import (
	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

// ServerContext is a lightweight dependency container passed to MCP tool
// handlers. It provides access to klausctl paths and runtime detection
// without requiring auth, federation, or instrumentation.
type ServerContext struct {
	Paths *config.Paths
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
