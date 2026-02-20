package cmd

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestExtractMCPText(t *testing.T) {
	tests := []struct {
		name   string
		result *mcp.CallToolResult
		want   string
	}{
		{
			name:   "nil result",
			result: nil,
			want:   "",
		},
		{
			name:   "empty content",
			result: &mcp.CallToolResult{},
			want:   "",
		},
		{
			name: "single text content",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: "hello"},
				},
			},
			want: "hello",
		},
		{
			name: "multiple text content items",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: "line1"},
					mcp.TextContent{Type: "text", Text: "line2"},
				},
			},
			want: "line1\nline2",
		},
		{
			name: "non-text content is skipped",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.ImageContent{Type: "image", MIMEType: "image/png", Data: "abc"},
				},
			},
			want: "",
		},
		{
			name: "mixed content types",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: "before"},
					mcp.ImageContent{Type: "image", MIMEType: "image/png", Data: "abc"},
					mcp.TextContent{Type: "text", Text: "after"},
				},
			},
			want: "before\nafter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractMCPText(tt.result)
			if got != tt.want {
				t.Errorf("extractMCPText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseAgentStatusField(t *testing.T) {
	tests := []struct {
		name   string
		result *mcp.CallToolResult
		want   string
	}{
		{
			name:   "nil result",
			result: nil,
			want:   "",
		},
		{
			name: "valid JSON with status field",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: `{"status":"completed","result":"done"}`},
				},
			},
			want: "completed",
		},
		{
			name: "non-JSON text returns raw text",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: "some plain text"},
				},
			},
			want: "some plain text",
		},
		{
			name: "JSON without status field returns raw text",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: `{"other":"field"}`},
				},
			},
			want: `{"other":"field"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAgentStatusField(tt.result)
			if got != tt.want {
				t.Errorf("parseAgentStatusField() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestColorStatus(t *testing.T) {
	colorEnabled = false
	t.Cleanup(func() { colorEnabled = detectColor() })

	tests := []struct {
		input string
		want  string
	}{
		{"started", "started"},
		{"completed", "completed"},
		{"error", "error"},
		{"failed", "failed"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := colorStatus(tt.input)
			if got != tt.want {
				t.Errorf("colorStatus(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
