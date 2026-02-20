package renderer

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

// renderMCPConfig writes the .mcp.json file containing MCP server configuration.
// The format wraps servers under "mcpServers" key, matching the Claude Code
// expected format (same as the Helm chart's rendering).
//
// Claude Code requires an explicit "type" field ("http" or "stdio") for each
// server entry. Without it, HTTP servers are misidentified as stdio, causing
// the subprocess to hang. This function infers the type from the entry fields
// when not explicitly set.
func (r *Renderer) renderMCPConfig(servers map[string]any) error {
	enriched := make(map[string]any, len(servers))
	for name, v := range servers {
		enriched[name] = inferMCPServerType(v)
	}

	data := map[string]any{
		"mcpServers": enriched,
	}

	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling MCP config: %w", err)
	}

	path := filepath.Join(r.paths.RenderedDir, "mcp-config.json")
	return writeFile(path, append(content, '\n'), 0o644)
}

// inferMCPServerType adds a "type" field to an MCP server entry when missing.
// Entries with a "url" field are classified as "http"; entries with a "command"
// field are classified as "stdio".
func inferMCPServerType(entry any) any {
	m, ok := entry.(map[string]any)
	if !ok {
		return entry
	}
	if _, hasType := m["type"]; hasType {
		return m
	}

	var inferredType string
	if _, hasURL := m["url"]; hasURL {
		inferredType = "http"
	} else if _, hasCmd := m["command"]; hasCmd {
		inferredType = "stdio"
	}
	if inferredType == "" {
		return m
	}

	enriched := make(map[string]any, len(m)+1)
	for k, v := range m {
		enriched[k] = v
	}
	enriched["type"] = inferredType
	return enriched
}
