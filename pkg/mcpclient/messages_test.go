package mcpclient

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

const (
	roleUser      = "user"
	roleAssistant = "assistant"
	roleTool      = "tool"

	fieldRole    = "role"
	fieldContent = "content"
)

// conversation builds the envelope the agent returns for a run of n messages
// fetched with the given offset: roles cycle user → assistant → tool and
// message i carries the body "msg-i". total is always n, as the agent reports
// the full count regardless of offset.
func conversation(t *testing.T, n, offset int) string {
	t.Helper()
	roles := []string{roleUser, roleAssistant, roleTool}
	msgs := make([]map[string]any, 0, n-offset)
	for i := offset; i < n; i++ {
		msgs = append(msgs, map[string]any{fieldRole: roles[i%3], fieldContent: fmt.Sprintf("msg-%d", i)})
	}
	env := map[string]any{
		keyMessages: msgs,
		keyMetadata: map[string]any{"model": "claude-opus-4-6"},
		keyTotal:    n,
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type shapedMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type shapedEnvelope struct {
	Messages   []shapedMessage `json:"messages"`
	Metadata   map[string]any  `json:"metadata"`
	Total      int             `json:"total"`
	Matched    int             `json:"matched"`
	Returned   int             `json:"returned"`
	NextOffset int             `json:"next_offset"`
	Truncated  *bool           `json:"truncated"`
}

func shape(t *testing.T, text string, opts *MessagesOpts) shapedEnvelope {
	t.Helper()
	out, ok := ShapeMessagesText(text, opts)
	if !ok {
		t.Fatalf("ShapeMessagesText returned ok=false for %q", text)
	}
	var env shapedEnvelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("shaped output is not a JSON envelope: %v\n%s", err, out)
	}
	return env
}

func contents(msgs []shapedMessage) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Content)
	}
	return out
}

func TestShapeMessagesTailOnLongRun(t *testing.T) {
	// 400 messages, roles cycling user/assistant/tool: assistant messages sit
	// at indices 1, 4, ..., 397 (133 of them).
	env := shape(t, conversation(t, 400, 0), &MessagesOpts{Types: roleAssistant, Tail: true, Limit: 3})

	if env.Total != 400 {
		t.Errorf("total = %d, want 400", env.Total)
	}
	if env.Matched != 133 {
		t.Errorf("matched = %d, want 133", env.Matched)
	}
	if env.Returned != 3 || len(env.Messages) != 3 {
		t.Fatalf("returned = %d (len %d), want 3", env.Returned, len(env.Messages))
	}
	if got := strings.Join(contents(env.Messages), ","); got != "msg-391,msg-394,msg-397" {
		t.Errorf("messages = %s, want msg-391,msg-394,msg-397", got)
	}
	for _, m := range env.Messages {
		if m.Role != roleAssistant {
			t.Errorf("role = %q, want %q", m.Role, roleAssistant)
		}
	}
	if env.NextOffset != 398 {
		t.Errorf("next_offset = %d, want 398", env.NextOffset)
	}
	if env.Truncated != nil {
		t.Errorf("truncated should be absent without maxChars, got %v", *env.Truncated)
	}
	if env.Metadata["model"] != "claude-opus-4-6" {
		t.Errorf("metadata not preserved: %v", env.Metadata)
	}
}

func TestShapeMessagesTailWithoutTypes(t *testing.T) {
	env := shape(t, conversation(t, 400, 0), &MessagesOpts{Tail: true, Limit: 2})
	if got := strings.Join(contents(env.Messages), ","); got != "msg-398,msg-399" {
		t.Errorf("messages = %s, want msg-398,msg-399", got)
	}
	if env.Matched != 400 || env.NextOffset != 400 {
		t.Errorf("matched/next_offset = %d/%d, want 400/400", env.Matched, env.NextOffset)
	}
}

func TestShapeMessagesTailDefaultLimit(t *testing.T) {
	env := shape(t, conversation(t, 400, 0), &MessagesOpts{Tail: true})
	if env.Returned != DefaultTailLimit {
		t.Fatalf("returned = %d, want %d", env.Returned, DefaultTailLimit)
	}
	if env.Messages[0].Content != "msg-390" {
		t.Errorf("first = %q, want msg-390", env.Messages[0].Content)
	}
}

func TestShapeMessagesOffsetLimitPaging(t *testing.T) {
	// Page 1: the agent applied offset 0.
	page := shape(t, conversation(t, 400, 0), &MessagesOpts{Limit: 50})
	if page.Returned != 50 || page.Messages[0].Content != "msg-0" || page.Messages[49].Content != "msg-49" {
		t.Fatalf("page 1 = %d messages %v", page.Returned, contents(page.Messages))
	}
	if page.NextOffset != 50 || page.Total != 400 {
		t.Errorf("page 1 next_offset/total = %d/%d, want 50/400", page.NextOffset, page.Total)
	}

	// Page 2: the caller passes next_offset; the agent trims the window.
	page = shape(t, conversation(t, 400, page.NextOffset), &MessagesOpts{Offset: 50, Limit: 50})
	if page.Messages[0].Content != "msg-50" || page.Messages[49].Content != "msg-99" {
		t.Errorf("page 2 = %v", contents(page.Messages))
	}
	if page.NextOffset != 100 || page.Matched != 350 {
		t.Errorf("page 2 next_offset/matched = %d/%d, want 100/350", page.NextOffset, page.Matched)
	}

	// Last page: fewer than limit remain.
	page = shape(t, conversation(t, 400, 390), &MessagesOpts{Offset: 390, Limit: 50})
	if page.Returned != 10 || page.NextOffset != 400 || page.Total != 400 {
		t.Errorf("last page returned/next_offset/total = %d/%d/%d, want 10/400/400", page.Returned, page.NextOffset, page.Total)
	}

	// Past the end: the agent returns an empty window.
	page = shape(t, conversation(t, 400, 400), &MessagesOpts{Offset: 400, Limit: 50})
	if page.Returned != 0 || page.NextOffset != 400 || len(page.Messages) != 0 {
		t.Errorf("empty page = %+v", page)
	}
}

func TestShapeMessagesOffsetWithTypesPaging(t *testing.T) {
	// Paging a filtered view: next_offset points right after the last
	// returned message, so resuming there never repeats or skips a match.
	page := shape(t, conversation(t, 400, 0), &MessagesOpts{Types: roleTool, Limit: 2})
	if got := strings.Join(contents(page.Messages), ","); got != "msg-2,msg-5" {
		t.Fatalf("page 1 = %s", got)
	}
	if page.NextOffset != 6 {
		t.Fatalf("next_offset = %d, want 6", page.NextOffset)
	}
	page = shape(t, conversation(t, 400, page.NextOffset), &MessagesOpts{Offset: page.NextOffset, Types: roleTool, Limit: 2})
	if got := strings.Join(contents(page.Messages), ","); got != "msg-8,msg-11" {
		t.Errorf("page 2 = %s", got)
	}
}

func TestShapeMessagesLimitLargerThanRun(t *testing.T) {
	env := shape(t, conversation(t, 5, 0), &MessagesOpts{Limit: 50})
	if env.Returned != 5 || env.NextOffset != 5 || env.Total != 5 {
		t.Errorf("returned/next_offset/total = %d/%d/%d, want 5/5/5", env.Returned, env.NextOffset, env.Total)
	}
}

func TestShapeMessagesTruncation(t *testing.T) {
	long := strings.Repeat("x", 300)
	text := fmt.Sprintf(`{"messages":[`+
		`{"role":"assistant","content":%q,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"Bash","arguments":%q}}]},`+
		`{"role":"tool","tool_call_id":"call_1","content":"short"}`+
		`],"metadata":{"model":%q},"total":2}`, long, long, long)

	out, ok := ShapeMessagesText(text, &MessagesOpts{MaxChars: 20})
	if !ok {
		t.Fatal("expected shaping")
	}

	var env struct {
		Messages []struct {
			Role       string `json:"role"`
			Content    string `json:"content"`
			ToolCallID string `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
		Metadata  map[string]string `json:"metadata"`
		Truncated bool              `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}

	wantBody := strings.Repeat("x", 20) + " ...[truncated 280 chars]"
	if env.Messages[0].Content != wantBody {
		t.Errorf("content = %q, want %q", env.Messages[0].Content, wantBody)
	}
	if env.Messages[0].ToolCalls[0].Function.Arguments != wantBody {
		t.Errorf("arguments = %q, want %q", env.Messages[0].ToolCalls[0].Function.Arguments, wantBody)
	}
	// Structural fields survive even though they are longer than maxChars
	// would allow for a body.
	if env.Messages[0].Role != roleAssistant || env.Messages[0].ToolCalls[0].ID != "call_1" || env.Messages[0].ToolCalls[0].Function.Name != "Bash" {
		t.Errorf("structural fields altered: %+v", env.Messages[0])
	}
	if env.Messages[1].Content != "short" || env.Messages[1].ToolCallID != "call_1" {
		t.Errorf("short message altered: %+v", env.Messages[1])
	}
	if !env.Truncated {
		t.Error("truncated = false, want true")
	}
	// Metadata is not a message body and stays intact.
	if env.Metadata["model"] != long {
		t.Error("metadata was truncated")
	}
}

func TestShapeMessagesTruncationRuneSafe(t *testing.T) {
	text := fmt.Sprintf(`{"messages":[{"role":"user","content":%q}],"total":1}`, strings.Repeat("ä", 30))
	env := shape(t, text, &MessagesOpts{MaxChars: 10})
	want := strings.Repeat("ä", 10) + " ...[truncated 20 chars]"
	if env.Messages[0].Content != want {
		t.Errorf("content = %q, want %q", env.Messages[0].Content, want)
	}
}

func TestShapeMessagesNoTruncationNeeded(t *testing.T) {
	text := `{"messages":[{"role":"user","content":"hey"}],"total":1}`
	env := shape(t, text, &MessagesOpts{MaxChars: 1000})
	if env.Truncated == nil || *env.Truncated {
		t.Errorf("truncated = %v, want false", env.Truncated)
	}
	if env.Messages[0].Content != "hey" {
		t.Errorf("content = %q", env.Messages[0].Content)
	}
}

func TestShapeMessagesStreamJSONTypes(t *testing.T) {
	text := `{"messages":[` +
		`{"type":"system","subtype":"init"},` +
		`{"type":"user","message":{"role":"user","content":"hi"}},` +
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hey"}]}}` +
		`],"total":3}`
	out, ok := ShapeMessagesText(text, &MessagesOpts{Types: "assistant, system"})
	if !ok {
		t.Fatal("expected shaping")
	}
	var env struct {
		Messages []struct {
			Type string `json:"type"`
		} `json:"messages"`
		Matched int `json:"matched"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if env.Matched != 2 || len(env.Messages) != 2 || env.Messages[0].Type != "system" || env.Messages[1].Type != roleAssistant {
		t.Errorf("unexpected selection: %s", out)
	}
}

func TestShapeMessagesTotalAbsent(t *testing.T) {
	text := `{"messages":[{"role":"user","content":"a"},{"role":"user","content":"b"},{"role":"user","content":"c"}]}`
	env := shape(t, text, &MessagesOpts{Offset: 5, Limit: 2})
	if env.Total != 8 {
		t.Errorf("total = %d, want offset+len = 8", env.Total)
	}
	if env.NextOffset != 7 {
		t.Errorf("next_offset = %d, want 7", env.NextOffset)
	}
}

func TestShapeMessagesJSONShape(t *testing.T) {
	text := `{"messages":[{"role":"user","content":"a"},{"role":"assistant","content":"b"}],"metadata":{"model":"m"},"total":2,"status":"running"}`
	out, ok := ShapeMessagesText(text, &MessagesOpts{Limit: 1, MaxChars: 5})
	if !ok {
		t.Fatal("expected shaping")
	}
	want := `{"messages":[{"role":"user","content":"a"}],"metadata":{"model":"m"},"total":2,"matched":2,"returned":1,"next_offset":1,"truncated":false,"status":"running"}`
	if out != want {
		t.Errorf("shaped envelope\n got: %s\nwant: %s", out, want)
	}
}

func TestShapeMessagesPassthrough(t *testing.T) {
	tests := []struct {
		name string
		text string
		opts *MessagesOpts
	}{
		{name: "nil opts", text: `{"messages":[],"total":0}`, opts: nil},
		{name: "offset only is agent-side", text: `{"messages":[],"total":0}`, opts: &MessagesOpts{Offset: 3}},
		{name: "plain text", text: "instance not running", opts: &MessagesOpts{Limit: 3}},
		{name: "object without messages", text: `{"status":"ok"}`, opts: &MessagesOpts{Limit: 3}},
		{name: "messages not an array", text: `{"messages":"nope"}`, opts: &MessagesOpts{Limit: 3}},
		{name: "json array", text: `[1,2,3]`, opts: &MessagesOpts{Limit: 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, ok := ShapeMessagesText(tt.text, tt.opts)
			if ok || out != tt.text {
				t.Errorf("ShapeMessagesText(%q) = %q, %v; want unchanged, false", tt.text, out, ok)
			}
		})
	}
}

func TestShapeMessagesResult(t *testing.T) {
	envelope := conversation(t, 400, 0)

	if got := ShapeMessages(nil, &MessagesOpts{Tail: true}); got != nil {
		t.Errorf("nil result should stay nil, got %+v", got)
	}

	errResult := mcp.NewToolResultError("boom")
	if got := ShapeMessages(errResult, &MessagesOpts{Tail: true}); got != errResult {
		t.Error("error results must pass through untouched")
	}

	plain := mcp.NewToolResultText(envelope)
	if got := ShapeMessages(plain, nil); got != plain {
		t.Error("nil opts must pass through untouched")
	}

	shaped := ShapeMessages(plain, &MessagesOpts{Types: roleAssistant, Tail: true, Limit: 3})
	if shaped == plain {
		t.Fatal("expected a new result")
	}
	var env shapedEnvelope
	if err := json.Unmarshal([]byte(ExtractText(shaped)), &env); err != nil {
		t.Fatal(err)
	}
	if env.Returned != 3 || env.Total != 400 || env.Messages[2].Content != "msg-397" {
		t.Errorf("unexpected shaped result: %+v", env)
	}
}
