package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

const (
	argOffset = "offset"
	testInst  = "inst"
)

// messagesAgent is an in-process MCP server exposing the agent's messages
// tool the way klaus does: it honours offset, ignores every other argument
// and reports total as the full converted count.
type messagesAgent struct {
	mu       sync.Mutex
	lastArgs map[string]any
	total    int
}

func (a *messagesAgent) handle(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	a.mu.Lock()
	a.lastArgs = req.GetArguments()
	a.mu.Unlock()

	offset := int(req.GetFloat(argOffset, 0))
	roles := []string{roleUser, roleAssistant, roleTool}
	msgs := make([]map[string]any, 0)
	for i := offset; i < a.total; i++ {
		msgs = append(msgs, map[string]any{fieldRole: roles[i%3], fieldContent: fmt.Sprintf("msg-%d", i)})
	}
	b, err := json.Marshal(map[string]any{keyMessages: msgs, keyMetadata: map[string]any{"model": "m"}, keyTotal: a.total})
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(b)), nil
}

func (a *messagesAgent) args() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastArgs
}

func startMessagesAgent(t *testing.T, total int) (*messagesAgent, string) {
	t.Helper()
	agent := &messagesAgent{total: total}
	srv := mcpserver.NewMCPServer("fake-klaus", "0.0.0", mcpserver.WithToolCapabilities(false))
	srv.AddTool(mcp.NewTool(keyMessages, mcp.WithNumber(argOffset)), agent.handle)
	ts := httptest.NewServer(mcpserver.NewStreamableHTTPServer(srv))
	t.Cleanup(ts.Close)
	return agent, ts.URL
}

func TestClientMessagesShapesOverHTTP(t *testing.T) {
	agent, url := startMessagesAgent(t, 400)
	c := New("test")
	t.Cleanup(c.Close)
	ctx := context.Background()

	// Without options the agent's envelope passes through untouched.
	res, err := c.Messages(ctx, testInst, url, nil)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	var plain map[string]json.RawMessage
	if err := json.Unmarshal([]byte(ExtractText(res)), &plain); err != nil {
		t.Fatalf("envelope: %v", err)
	}
	if _, ok := plain[keyReturned]; ok {
		t.Errorf("default call must not add shaping fields: %s", ExtractText(res))
	}
	if len(plain) != 3 {
		t.Errorf("default envelope keys = %d, want messages/metadata/total", len(plain))
	}

	// Health peek: only types travels to the agent; tail, limit and maxChars
	// are applied by klausctl on the way back.
	res, err = c.Messages(ctx, testInst, url, &MessagesOpts{Types: roleAssistant, Tail: true, Limit: 3, MaxChars: 5})
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	var env shapedEnvelope
	if err := json.Unmarshal([]byte(ExtractText(res)), &env); err != nil {
		t.Fatalf("shaped envelope: %v", err)
	}
	if env.Total != 400 || env.Matched != 133 || env.Returned != 3 || env.NextOffset != 398 {
		t.Errorf("total/matched/returned/next_offset = %d/%d/%d/%d, want 400/133/3/398", env.Total, env.Matched, env.Returned, env.NextOffset)
	}
	if got := env.Messages[2].Content; got != "msg-3 ...[truncated 2 chars]" {
		t.Errorf("last content = %q", got)
	}
	if env.Truncated == nil || !*env.Truncated {
		t.Error("truncated should be true")
	}
	args := agent.args()
	if args["types"] != roleAssistant {
		t.Errorf("types not forwarded: %v", args)
	}
	for _, local := range []string{argOffset, "limit", "tail", "maxChars"} {
		if _, ok := args[local]; ok {
			t.Errorf("%s must not be sent to the agent: %v", local, args)
		}
	}

	// Paging: offset is the agent's job and travels with the call.
	res, err = c.Messages(ctx, testInst, url, &MessagesOpts{Offset: 398, Limit: 10})
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	env = shapedEnvelope{}
	if err := json.Unmarshal([]byte(ExtractText(res)), &env); err != nil {
		t.Fatalf("shaped envelope: %v", err)
	}
	if env.Returned != 2 || env.NextOffset != 400 || env.Messages[0].Content != "msg-398" {
		t.Errorf("page = %+v", env)
	}
	if got := agent.args()[argOffset]; got != float64(398) {
		t.Errorf("offset forwarded = %v, want 398", got)
	}
}
