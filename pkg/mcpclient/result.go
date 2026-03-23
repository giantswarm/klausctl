package mcpclient

import (
	"encoding/json"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// ExtractText returns the concatenated text content from an MCP tool result.
// Only TextContent items are extracted; non-text content types (images, etc.)
// are silently skipped.
func ExtractText(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}

	var parts []string
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// ParseStatusField extracts the "status" JSON field from an MCP tool result.
// Returns the status string if found, or empty string otherwise.
func ParseStatusField(result *mcp.CallToolResult) string {
	text := ExtractText(result)
	if text == "" {
		return ""
	}
	var parsed struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err == nil {
		return parsed.Status
	}
	return ""
}

// IsTerminalStatus reports whether the given agent status indicates that the
// task is finished (successfully or otherwise).
func IsTerminalStatus(status string) bool {
	switch status {
	case "completed", "error", "failed":
		return true
	default:
		return false
	}
}
