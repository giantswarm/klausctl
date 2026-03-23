package mcpclient

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestExtractText(t *testing.T) {
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
			got := ExtractText(tt.result)
			if got != tt.want {
				t.Errorf("ExtractText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseStatusField(t *testing.T) {
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
			name:   "json with status field",
			result: mcp.NewToolResultText(`{"status":"completed","detail":"all done"}`),
			want:   "completed",
		},
		{
			name:   "json with error status",
			result: mcp.NewToolResultText(`{"status":"error","message":"something broke"}`),
			want:   "error",
		},
		{
			name:   "non-json text returns empty",
			result: mcp.NewToolResultText("some plain text"),
			want:   "",
		},
		{
			name:   "json without status field returns empty",
			result: mcp.NewToolResultText(`{"message":"no status here"}`),
			want:   "",
		},
		{
			name:   "json with empty status returns empty",
			result: mcp.NewToolResultText(`{"status":""}`),
			want:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseStatusField(tt.result)
			if got != tt.want {
				t.Errorf("ParseStatusField() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsTerminalStatus(t *testing.T) {
	for _, status := range []string{"completed", "error", "failed"} {
		if !IsTerminalStatus(status) {
			t.Errorf("expected %q to be terminal", status)
		}
	}
	for _, status := range []string{"running", "idle", "processing", "busy", ""} {
		if IsTerminalStatus(status) {
			t.Errorf("expected %q to NOT be terminal", status)
		}
	}
}
