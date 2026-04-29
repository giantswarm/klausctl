package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestParseResultResponse(t *testing.T) {
	tests := []struct {
		name    string
		result  *mcp.CallToolResult
		wantCLI resultCLIResult
	}{
		{
			name: "completed agent with result text",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: `{"status":"completed","message_count":42,"result_text":"All tests passed."}`,
					},
				},
			},
			wantCLI: resultCLIResult{
				Instance:     "dev",
				Status:       "completed",
				MessageCount: 42,
				Result:       "All tests passed.",
			},
		},
		{
			name: "busy agent with message count",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: `{"status":"busy","message_count":15,"result_text":""}`,
					},
				},
			},
			wantCLI: resultCLIResult{
				Instance:     "dev",
				Status:       "busy",
				MessageCount: 15,
				Result:       "",
			},
		},
		{
			name: "idle agent with zero messages",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: `{"status":"idle","message_count":0,"result_text":""}`,
					},
				},
			},
			wantCLI: resultCLIResult{
				Instance:     "dev",
				Status:       "idle",
				MessageCount: 0,
				Result:       "",
			},
		},
		{
			name: "error from IsError flag",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: "something went wrong"},
				},
				IsError: true,
			},
			wantCLI: resultCLIResult{
				Instance: "dev",
				Status:   "error",
				Result:   "something went wrong",
			},
		},
		{
			name: "non-JSON fallback returns completed",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: "plain text result"},
				},
			},
			wantCLI: resultCLIResult{
				Instance: "dev",
				Status:   "completed",
				Result:   "plain text result",
			},
		},
		{
			name: "JSON without status field falls back to completed",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: `{"other":"field"}`},
				},
			},
			wantCLI: resultCLIResult{
				Instance: "dev",
				Status:   "completed",
				Result:   `{"other":"field"}`,
			},
		},
		{
			name:   "nil result",
			result: nil,
			wantCLI: resultCLIResult{
				Instance: "dev",
				Status:   "completed",
				Result:   "",
			},
		},
		{
			name: "error status from agent JSON",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: `{"status":"error","message_count":5,"result_text":"Build failed"}`,
					},
				},
			},
			wantCLI: resultCLIResult{
				Instance:     "dev",
				Status:       "error",
				MessageCount: 5,
				Result:       "Build failed",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseResultResponse("dev", tt.result)
			if got.Instance != tt.wantCLI.Instance {
				t.Errorf("Instance = %q, want %q", got.Instance, tt.wantCLI.Instance)
			}
			if got.Status != tt.wantCLI.Status {
				t.Errorf("Status = %q, want %q", got.Status, tt.wantCLI.Status)
			}
			if got.MessageCount != tt.wantCLI.MessageCount {
				t.Errorf("MessageCount = %d, want %d", got.MessageCount, tt.wantCLI.MessageCount)
			}
			if got.Result != tt.wantCLI.Result {
				t.Errorf("Result = %q, want %q", got.Result, tt.wantCLI.Result)
			}
		})
	}
}

func TestRenderResultOutput_Text(t *testing.T) {
	colorEnabled = false
	t.Cleanup(func() { colorEnabled = detectColor() })

	tests := []struct {
		name     string
		result   resultCLIResult
		contains []string
	}{
		{
			name: "completed with result",
			result: resultCLIResult{
				Instance:     "dev",
				Status:       "completed",
				MessageCount: 42,
				Result:       "All tests passed.",
			},
			contains: []string{
				"Instance: dev",
				"Status:   completed",
				"Messages: 42",
				"All tests passed.",
			},
		},
		{
			name: "busy shows progress hint",
			result: resultCLIResult{
				Instance:     "dev",
				Status:       "busy",
				MessageCount: 15,
			},
			contains: []string{
				"Instance: dev",
				"Status:   busy",
				"Messages: 15",
				"Agent is still processing",
				"klausctl result dev",
			},
		},
		{
			name: "idle with zero messages",
			result: resultCLIResult{
				Instance: "dev",
				Status:   "idle",
			},
			contains: []string{
				"Instance: dev",
				"Status:   idle",
				"Messages: 0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resultOutput = "text" //nolint:goconst
			var buf bytes.Buffer
			err := renderResultOutput(&buf, tt.result)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			output := buf.String()
			for _, want := range tt.contains {
				if !strings.Contains(output, want) {
					t.Errorf("output missing %q\ngot:\n%s", want, output)
				}
			}
		})
	}
}

func TestRenderResultOutput_JSON(t *testing.T) {
	resultOutput = "json"
	t.Cleanup(func() { resultOutput = "text" })

	result := resultCLIResult{
		Instance:     "dev",
		Status:       "completed",
		MessageCount: 42,
		Result:       "done",
	}

	var buf bytes.Buffer
	err := renderResultOutput(&buf, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded resultCLIResult
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\ngot:\n%s", err, buf.String())
	}

	if decoded.Instance != "dev" {
		t.Errorf("Instance = %q, want %q", decoded.Instance, "dev")
	}
	if decoded.Status != "completed" { //nolint:goconst
		t.Errorf("Status = %q, want %q", decoded.Status, "completed")
	}
	if decoded.MessageCount != 42 {
		t.Errorf("MessageCount = %d, want %d", decoded.MessageCount, 42)
	}
	if decoded.Result != "done" {
		t.Errorf("Result = %q, want %q", decoded.Result, "done")
	}
}
