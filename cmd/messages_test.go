package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// Message roles used in transcript fixtures.
const (
	roleUser      = "user"
	roleAssistant = "assistant"
)

func TestParseMessagesResponse(t *testing.T) {
	tests := []struct {
		name      string
		result    *mcp.CallToolResult
		wantCount int
		wantTotal int
	}{
		{
			name: "flat role/content format",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: outputText,
						Text: `{"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"}],"total":2}`,
					},
				},
			},
			wantCount: 2,
			wantTotal: 2,
		},
		{
			name: "nested agent format with content blocks",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: outputText,
						Text: `{"messages":[{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello"}]}},{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi there"}]}}],"total":2}`,
					},
				},
			},
			wantCount: 2,
			wantTotal: 2,
		},
		{
			name: "nested format skips system hook messages",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: outputText,
						Text: `{"messages":[{"type":"system","subtype":"hook_started"},{"type":"system","subtype":"init"},{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}],"total":3}`,
					},
				},
			},
			wantCount: 1,
			wantTotal: 3,
		},
		{
			name: "nested format with tool_use blocks",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: outputText,
						Text: `{"messages":[{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash"}]}}],"total":1}`,
					},
				},
			},
			wantCount: 1,
			wantTotal: 1,
		},
		{
			name: "result message type",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: outputText,
						Text: `{"messages":[{"type":"result","result":"All done."}],"total":1}`,
					},
				},
			},
			wantCount: 1,
			wantTotal: 1,
		},
		{
			name: "format with metadata",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: outputText,
						Text: `{"messages":[{"role":"user","content":"hello"}],"metadata":{"session_id":"abc","model":"claude-opus-4-6"},"total":5}`,
					},
				},
			},
			wantCount: 1,
			wantTotal: 5,
		},
		{
			name: "empty messages",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: outputText,
						Text: `{"messages":[],"total":0}`,
					},
				},
			},
			wantCount: 0,
			wantTotal: 0,
		},
		{
			name: "error from IsError flag",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: outputText, Text: testErrMsg},
				},
				IsError: true,
			},
			wantCount: 0,
			wantTotal: 0,
		},
		{
			name:      testNilResultMsg,
			result:    nil,
			wantCount: 0,
			wantTotal: 0,
		},
		{
			name: "non-JSON fallback",
			result: &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: outputText, Text: "plain text"},
				},
			},
			wantCount: 0,
			wantTotal: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMessagesResponse(tt.result)
			if len(got.Messages) != tt.wantCount {
				t.Errorf("Messages count = %d, want %d", len(got.Messages), tt.wantCount)
			}
			if got.Total != tt.wantTotal {
				t.Errorf("Total = %d, want %d", got.Total, tt.wantTotal)
			}
		})
	}
}

func TestConvertRawMessage(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantOk      bool
		wantRole    string
		wantContent string
	}{
		{
			name:        "flat role/content",
			input:       `{"role":"user","content":"hello"}`,
			wantOk:      true,
			wantRole:    roleUser,
			wantContent: testHello,
		},
		{
			name:        "nested assistant with text block",
			input:       `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hello world"}]}}`,
			wantOk:      true,
			wantRole:    roleAssistant,
			wantContent: testHelloWorld,
		},
		{
			name:        "nested user with text block",
			input:       `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"do something"}]}}`,
			wantOk:      true,
			wantRole:    roleUser,
			wantContent: "do something",
		},
		{
			name:        "nested assistant with tool_use",
			input:       `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash"}]}}`,
			wantOk:      true,
			wantRole:    roleAssistant,
			wantContent: "[tool_use: Bash]",
		},
		{
			name:        "nested assistant with mixed content",
			input:       `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Let me check"},{"type":"tool_use","name":"Read"}]}}`,
			wantOk:      true,
			wantRole:    roleAssistant,
			wantContent: "Let me check\n[tool_use: Read]",
		},
		{
			name:        "nested assistant with string content",
			input:       `{"type":"assistant","message":{"role":"assistant","content":"plain string"}}`,
			wantOk:      true,
			wantRole:    roleAssistant,
			wantContent: "plain string",
		},
		{
			name:   "system hook_started is skipped",
			input:  `{"type":"system","subtype":"hook_started","hook_name":"SessionStart:startup"}`,
			wantOk: false,
		},
		{
			name:   "system init is skipped",
			input:  `{"type":"system","subtype":"init","cwd":"/workspace"}`,
			wantOk: false,
		},
		{
			name:   "system hook_response is skipped",
			input:  `{"type":"system","subtype":"hook_response","output":"ok"}`,
			wantOk: false,
		},
		{
			name:        "result message",
			input:       `{"type":"result","result":"All tests passed."}`,
			wantOk:      true,
			wantRole:    roleSystem,
			wantContent: testAgentResult,
		},
		{
			name:   "result without text is skipped",
			input:  `{"type":"result","result":""}`,
			wantOk: false,
		},
		{
			name:   "assistant with empty content is skipped",
			input:  `{"type":"assistant","message":{"role":"assistant","content":[]}}`,
			wantOk: false,
		},
		{
			name:   "invalid json",
			input:  `not json`,
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, ok := convertRawMessage(json.RawMessage(tt.input))
			if ok != tt.wantOk {
				t.Fatalf("ok = %v, want %v (msg=%+v)", ok, tt.wantOk, msg)
			}
			if !ok {
				return
			}
			if msg.Role != tt.wantRole {
				t.Errorf("Role = %q, want %q", msg.Role, tt.wantRole)
			}
			if msg.Content != tt.wantContent {
				t.Errorf("Content = %q, want %q", msg.Content, tt.wantContent)
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
				Type: outputText,
				Text: `{"messages":[{"role":"user","content":"hello world"},{"role":"assistant","content":"hi there"}],"total":2}`,
			},
		},
	}

	messagesOutput = outputText
	var buf bytes.Buffer
	err := renderMessages(&buf, testInstanceDev, result, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	for _, want := range []string{
		testInstanceHeader,
		"Messages: 2",
		"[user]",
		testHelloWorld,
		"[assistant]",
		"hi there",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, output)
		}
	}
}

func TestRenderMessages_NestedFormat(t *testing.T) {
	colorEnabled = false
	t.Cleanup(func() { colorEnabled = detectColor() })

	result := &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: outputText,
				Text: `{"messages":[{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello world"}]}},{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi there"}]}}],"total":2}`,
			},
		},
	}

	messagesOutput = outputText
	var buf bytes.Buffer
	err := renderMessages(&buf, testInstanceDev, result, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	for _, want := range []string{
		testInstanceHeader,
		"Messages: 2",
		"[user]",
		testHelloWorld,
		"[assistant]",
		"hi there",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, output)
		}
	}
}

func TestRenderMessages_JSON(t *testing.T) {
	messagesOutput = outputJSON
	t.Cleanup(func() { messagesOutput = outputText })

	result := &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: outputText,
				Text: `{"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"world"}],"total":2}`,
			},
		},
	}

	var buf bytes.Buffer
	err := renderMessages(&buf, testInstanceDev, result, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded messagesCLIResult
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\ngot:\n%s", err, buf.String())
	}

	if decoded.Instance != testInstanceDev {
		t.Errorf("Instance = %q, want %q", decoded.Instance, testInstanceDev)
	}
	if decoded.Count != 2 {
		t.Errorf("Count = %d, want %d", decoded.Count, 2)
	}
	if len(decoded.Messages) != 2 {
		t.Fatalf("Messages length = %d, want %d", len(decoded.Messages), 2)
	}
	if decoded.Messages[0].Role != roleUser {
		t.Errorf("Messages[0].Role = %q, want %q", decoded.Messages[0].Role, roleUser)
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
				Type: outputText,
				Text: `{"messages":[],"total":0}`,
			},
		},
	}

	messagesOutput = outputText
	var buf bytes.Buffer
	err := renderMessages(&buf, testInstanceDev, result, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Messages: 0") {
		t.Errorf("expected 'Messages: 0' in output, got:\n%s", output)
	}
}

func TestRenderMessages_TotalGreaterThanMessages(t *testing.T) {
	colorEnabled = false
	t.Cleanup(func() { colorEnabled = detectColor() })

	result := &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: outputText,
				Text: `{"messages":[{"role":"user","content":"hello"}],"total":42}`,
			},
		},
	}

	messagesOutput = outputText
	var buf bytes.Buffer
	err := renderMessages(&buf, testInstanceDev, result, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Messages: 42") {
		t.Errorf("expected 'Messages: 42' in output (using total), got:\n%s", output)
	}
}
