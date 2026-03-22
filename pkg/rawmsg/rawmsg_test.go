package rawmsg

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConvert(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantRole    string
		wantContent string
	}{
		{
			name:        "assistant text",
			input:       `{"type":"assistant","subtype":"text","text":"Hello there","tool_name":"","timestamp":"2026-01-01T00:00:00Z"}`,
			wantRole:    "assistant",
			wantContent: "Hello there",
		},
		{
			name:        "assistant tool_use with name",
			input:       `{"type":"assistant","subtype":"tool_use","text":"","tool_name":"read_file","timestamp":"2026-01-01T00:00:00Z"}`,
			wantRole:    "assistant",
			wantContent: "[tool_use: read_file]",
		},
		{
			name:        "assistant tool_use with text",
			input:       `{"type":"assistant","subtype":"tool_use","text":"reading config","tool_name":"read_file","timestamp":"2026-01-01T00:00:00Z"}`,
			wantRole:    "assistant",
			wantContent: "[tool_use: read_file] reading config",
		},
		{
			name:        "assistant tool_use no name",
			input:       `{"type":"assistant","subtype":"tool_use","text":"","tool_name":""}`,
			wantRole:    "assistant",
			wantContent: "[tool_use: unknown]",
		},
		{
			name:        "tool_result",
			input:       `{"type":"tool_result","subtype":"","text":"file contents here","tool_name":""}`,
			wantRole:    "tool",
			wantContent: "file contents here",
		},
		{
			name:        "tool_result truncated",
			input:       `{"type":"tool_result","subtype":"","text":"` + strings.Repeat("x", 600) + `","tool_name":""}`,
			wantRole:    "tool",
			wantContent: strings.Repeat("x", 500) + "…",
		},
		{
			name:        "user message",
			input:       `{"type":"user","subtype":"","text":"do something","tool_name":""}`,
			wantRole:    "user",
			wantContent: "do something",
		},
		{
			name:        "system message",
			input:       `{"type":"system","subtype":"","text":"you are a helpful assistant","tool_name":""}`,
			wantRole:    "system",
			wantContent: "you are a helpful assistant",
		},
		{
			name:        "unknown type",
			input:       `{"type":"other","subtype":"","text":"something","tool_name":""}`,
			wantRole:    "other",
			wantContent: "something",
		},
		{
			name:        "invalid json",
			input:       `not json`,
			wantRole:    "unknown",
			wantContent: "not json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Convert(json.RawMessage(tt.input))
			if got.Role != tt.wantRole {
				t.Errorf("Role = %q, want %q", got.Role, tt.wantRole)
			}
			if got.Content != tt.wantContent {
				t.Errorf("Content = %q, want %q", got.Content, tt.wantContent)
			}
		})
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantOk     bool
		wantStatus string
		wantTotal  int
		wantCount  int
	}{
		{
			name:       "raw format with messages",
			input:      `{"status":"completed","total":3,"messages":[{"type":"user","text":"hello"},{"type":"assistant","subtype":"text","text":"hi"},{"type":"assistant","subtype":"tool_use","tool_name":"bash"}]}`,
			wantOk:     true,
			wantStatus: "completed",
			wantTotal:  3,
			wantCount:  3,
		},
		{
			name:       "raw format empty messages",
			input:      `{"status":"idle","total":0,"messages":[]}`,
			wantOk:     true,
			wantStatus: "idle",
			wantTotal:  0,
			wantCount:  0,
		},
		{
			name:       "raw format busy",
			input:      `{"status":"busy","total":5,"messages":[{"type":"user","text":"work"}]}`,
			wantOk:     true,
			wantStatus: "busy",
			wantTotal:  5,
			wantCount:  1,
		},
		{
			name:   "legacy format rejected",
			input:  `{"status":"completed","messages":[{"role":"user","content":"hello"}]}`,
			wantOk: false,
		},
		{
			name:   "invalid json",
			input:  `not json`,
			wantOk: false,
		},
		{
			name:   "empty status",
			input:  `{"status":"","total":0,"messages":[]}`,
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, total, msgs, ok := Parse(tt.input)
			if ok != tt.wantOk {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOk)
			}
			if !ok {
				return
			}
			if status != tt.wantStatus {
				t.Errorf("status = %q, want %q", status, tt.wantStatus)
			}
			if total != tt.wantTotal {
				t.Errorf("total = %d, want %d", total, tt.wantTotal)
			}
			if len(msgs) != tt.wantCount {
				t.Errorf("messages count = %d, want %d", len(msgs), tt.wantCount)
			}
		})
	}
}

func TestParseMessageContent(t *testing.T) {
	input := `{"status":"completed","total":2,"messages":[{"type":"user","text":"hello"},{"type":"assistant","subtype":"text","text":"world"}]}`
	_, _, msgs, ok := Parse(input)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Errorf("msgs[0] = %+v, want user/hello", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "world" {
		t.Errorf("msgs[1] = %+v, want assistant/world", msgs[1])
	}
}
