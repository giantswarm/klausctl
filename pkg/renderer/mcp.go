package renderer

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

// renderMCPConfig writes the .mcp.json file containing MCP server configuration.
// The format wraps servers under "mcpServers" key, matching the Claude Code
// expected format (same as the Helm chart's rendering).
func (r *Renderer) renderMCPConfig(servers map[string]any) error {
	data := map[string]any{
		"mcpServers": servers,
	}

	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling MCP config: %w", err)
	}

	path := filepath.Join(r.paths.RenderedDir, "mcp-config.json")
	return writeFile(path, append(content, '\n'), 0o644)
}
