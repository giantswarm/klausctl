package cmd

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/giantswarm/klausctl/pkg/mcpclient"
)

func TestExtractText(t *testing.T) {
	tests := []struct {
		name   string
		result *mcp.CallToolResult
		want   string
	}{
		{
			name:   testNilResultMsg,
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
					mcp.TextContent{Type: outputText, Text: testHello},
				},
			},
			want: testHello,
		},
		{
			name: "multiple text content items",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: outputText, Text: "line1"},
					mcp.TextContent{Type: outputText, Text: "line2"},
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
					mcp.TextContent{Type: outputText, Text: "before"},
					mcp.ImageContent{Type: "image", MIMEType: "image/png", Data: "abc"},
					mcp.TextContent{Type: outputText, Text: "after"},
				},
			},
			want: "before\nafter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mcpclient.ExtractText(tt.result)
			if got != tt.want {
				t.Errorf("ExtractText() = %q, want %q", got, tt.want)
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
		{statusStarted, statusStarted},
		{statusCompleted, statusCompleted},
		{statusIdle, statusIdle},
		{statusBusy, statusBusy},
		{statusError, statusError},
		{statusFailed, statusFailed},
		{unknownValue, unknownValue},
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
