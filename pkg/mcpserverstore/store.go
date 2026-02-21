// Package mcpserverstore manages the global registry of managed MCP server
// definitions for klausctl. Each server has a URL and an optional secret
// reference that is resolved at instance start time into a Bearer token header.
package mcpserverstore

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// McpServerDef describes a managed MCP server with a URL and optional
// secret reference used for authentication.
type McpServerDef struct {
	URL    string `yaml:"url"`
	Secret string `yaml:"secret,omitempty"`
}

// Store manages named MCP server definitions persisted as a YAML file.
type Store struct {
	path    string
	servers map[string]McpServerDef
}

// Load reads the MCP server definitions from the given file path. If the
// file does not exist, an empty store is returned.
func Load(path string) (*Store, error) {
	s := &Store{
		path:    path,
		servers: make(map[string]McpServerDef),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, fmt.Errorf("reading MCP servers file: %w", err)
	}

	if err := yaml.Unmarshal(data, &s.servers); err != nil {
		return nil, fmt.Errorf("parsing MCP servers file: %w", err)
	}
	if s.servers == nil {
		s.servers = make(map[string]McpServerDef)
	}

	return s, nil
}

// Save writes the current server definitions to disk.
func (s *Store) Save() error {
	data, err := yaml.Marshal(s.servers)
	if err != nil {
		return fmt.Errorf("marshaling MCP servers: %w", err)
	}
	return os.WriteFile(s.path, data, 0o644)
}

// Add registers or updates a managed MCP server definition.
func (s *Store) Add(name string, def McpServerDef) {
	s.servers[name] = def
}

// Get retrieves a server definition by name.
func (s *Store) Get(name string) (McpServerDef, error) {
	def, ok := s.servers[name]
	if !ok {
		return McpServerDef{}, fmt.Errorf("MCP server %q not found", name)
	}
	return def, nil
}

// Remove deletes a managed MCP server by name.
func (s *Store) Remove(name string) error {
	if _, ok := s.servers[name]; !ok {
		return fmt.Errorf("MCP server %q not found", name)
	}
	delete(s.servers, name)
	return nil
}

// List returns all server names in sorted order.
func (s *Store) List() []string {
	names := make([]string, 0, len(s.servers))
	for k := range s.servers {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// All returns a copy of all server definitions keyed by name.
func (s *Store) All() map[string]McpServerDef {
	cp := make(map[string]McpServerDef, len(s.servers))
	for k, v := range s.servers {
		cp[k] = v
	}
	return cp
}
