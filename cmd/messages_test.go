package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestParseMessagesResponse(t *testing.T) {
	tests := []struct {
		name       string
		result     *mcp.CallToolResult
		wantStatus string
		wantCount  int
	}{
		{
			name: "completed with messages",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: `{"status":"completed","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"}]}`,
					},
				},
			},
			wantStatus: "completed",
			wantCount:  2,
		},
		{
			name: "busy with messages",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: `{"status":"busy","messages":[{"role":"user","content":"do something"}]}`,
					},
				},
			},
			wantStatus: "busy",
			wantCount:  1,
		},
		{
			name: "idle with no messages",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: `{"status":"idle","messages":[]}`,
					},
				},
			},
			wantStatus: "idle",
			wantCount:  0,
		},
		{
			name: "error from IsError flag",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: "something went wrong"},
				},
				IsError: true,
			},
			wantStatus: "error",
			wantCount:  0,
		},
		{
			name:       "nil result",
			result:     nil,
			wantStatus: "unknown",
			wantCount:  0,
		},
		{
			name: "non-JSON fallback",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: "plain text"},
				},
			},
			wantStatus: "unknown",
			wantCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMessagesResponse(tt.result)
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", got.Status, tt.wantStatus)
			}
			if len(got.Messages) != tt.wantCount {
				t.Errorf("Messages count = %d, want %d", len(got.Messages), tt.wantCount)
			}
		})
	}
}

func TestRenderMessages_Text(t *testing.T) {
	colorEnabled = false
	t.Cleanup(func() { colorEnabled = detectColor() })

	result := &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: `{"status":"completed","messages":[{"role":"user","content":"hello world"},{"role":"assistant","content":"hi there"}]}`,
			},
		},
	}

	messagesOutput = "text"
	var buf bytes.Buffer
	err := renderMessages(&buf, "dev", result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	for _, want := range []string{
		"Instance: dev",
		"Status:   completed",
		"Messages: 2",
		"[user]",
		"hello world",
		"[assistant]",
		"hi there",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, output)
		}
	}
}

func TestRenderMessages_JSON(t *testing.T) {
	messagesOutput = "json"
	t.Cleanup(func() { messagesOutput = "text" })

	result := &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: `{"status":"completed","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"world"}]}`,
			},
		},
	}

	var buf bytes.Buffer
	err := renderMessages(&buf, "dev", result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded messagesCLIResult
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\ngot:\n%s", err, buf.String())
	}

	if decoded.Instance != "dev" {
		t.Errorf("Instance = %q, want %q", decoded.Instance, "dev")
	}
	if decoded.Status != "completed" {
		t.Errorf("Status = %q, want %q", decoded.Status, "completed")
	}
	if decoded.Count != 2 {
		t.Errorf("Count = %d, want %d", decoded.Count, 2)
	}
	if len(decoded.Messages) != 2 {
		t.Fatalf("Messages length = %d, want %d", len(decoded.Messages), 2)
	}
	if decoded.Messages[0].Role != "user" {
		t.Errorf("Messages[0].Role = %q, want %q", decoded.Messages[0].Role, "user")
	}
	if decoded.Messages[1].Content != "world" {
		t.Errorf("Messages[1].Content = %q, want %q", decoded.Messages[1].Content, "world")
	}
}

func TestRenderMessages_Empty(t *testing.T) {
	colorEnabled = false
	t.Cleanup(func() { colorEnabled = detectColor() })

	result := &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: `{"status":"idle","messages":[]}`,
			},
		},
	}

	messagesOutput = "text"
	var buf bytes.Buffer
	err := renderMessages(&buf, "dev", result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Messages: 0") {
		t.Errorf("expected 'Messages: 0' in output, got:\n%s", output)
	}
}
