package instance

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestHandlePromptMissingName(t *testing.T) {
	sc := testServerContext(t)
	req := callToolRequest(map[string]any{"message": "hello"})
	result, err := handlePrompt(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertIsError(t, result)
}

func TestHandlePromptMissingMessage(t *testing.T) {
	sc := testServerContext(t)
	req := callToolRequest(map[string]any{"name": "test"})
	result, err := handlePrompt(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertIsError(t, result)
}

func TestHandlePromptInstanceNotFound(t *testing.T) {
	sc := testServerContext(t)
	req := callToolRequest(map[string]any{
		"name":    "nonexistent",
		"message": "hello",
	})
	result, err := handlePrompt(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertIsError(t, result)
}

func TestHandleResultMissingName(t *testing.T) {
	sc := testServerContext(t)
	req := callToolRequest(map[string]any{})
	result, err := handleResult(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertIsError(t, result)
}

func TestHandleResultInstanceNotFound(t *testing.T) {
	sc := testServerContext(t)
	req := callToolRequest(map[string]any{"name": "nonexistent"})
	result, err := handleResult(context.Background(), req, sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertIsError(t, result)
}

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
			name:   "text content",
			result: mcp.NewToolResultText("hello world"),
			want:   "hello world",
		},
		{
			name: "multiple text contents",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent("line 1"),
					mcp.NewTextContent("line 2"),
				},
			},
			want: "line 1\nline 2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractText(tt.result)
			if got != tt.want {
				t.Errorf("extractText() = %q, want %q", got, tt.want)
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
			name:   "non-json text falls through",
			result: mcp.NewToolResultText("some plain text"),
			want:   "some plain text",
		},
		{
			name:   "json without status field falls through",
			result: mcp.NewToolResultText(`{"message":"no status here"}`),
			want:   `{"message":"no status here"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseStatusField(tt.result)
			if got != tt.want {
				t.Errorf("parseStatusField() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTerminalStatuses(t *testing.T) {
	for _, status := range []string{"completed", "error", "failed"} {
		if !terminalStatuses[status] {
			t.Errorf("expected %q to be terminal", status)
		}
	}
	for _, status := range []string{"running", "idle", "processing"} {
		if terminalStatuses[status] {
			t.Errorf("expected %q to NOT be terminal", status)
		}
	}
}

func TestAgentBaseURLInstanceNotFound(t *testing.T) {
	sc := testServerContext(t)
	_, err := agentBaseURL(context.Background(), "nonexistent", sc)
	if err == nil {
		t.Fatal("expected error for missing instance")
	}
}
