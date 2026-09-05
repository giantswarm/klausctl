package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// fakeAgent is a minimal MCP streamable-HTTP server standing in for a klaus
// agent instance. It poses as a legacy server, since protocol versions before
// 2026-07-28 are the only ones that keep server-side session state and thus
// the only ones where a cached session can go stale. It can be told to answer
// tools/call with 404, the way a restarted agent rejects a session it no
// longer knows.
type fakeAgent struct {
	mu            sync.Mutex
	initializes   int
	toolCalls     int
	terminateAll  bool
	terminateOnce bool
	// modern makes the agent answer the server/discover probe, so the client
	// negotiates the stateless protocol instead of the initialize handshake.
	modern            bool
	discovers         int
	sessionHeaderSeen bool
}

func (f *fakeAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// No standalone SSE stream, and nothing to do for the DELETE that
		// Close sends.
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if r.Header.Get(transport.HeaderKeySessionID) != "" {
		f.sessionHeaderSeen = true
	}

	switch req.Method {
	case string(mcp.MethodServerDiscover):
		f.discovers++
		if f.modern {
			writeRPCResult(w, req.ID, fmt.Sprintf(
				`{"supportedVersions":[%q],"capabilities":{"tools":{}}}`,
				mcp.LATEST_PROTOCOL_VERSION))
			return
		}
		// Refusing the probe sends the client down the initialize handshake.
		writeRPCError(w, req.ID)
	case "initialize":
		f.initializes++
		w.Header().Set(transport.HeaderKeySessionID, fmt.Sprintf("session-%d", f.initializes))
		writeRPCResult(w, req.ID, fmt.Sprintf(
			`{"protocolVersion":%q,"capabilities":{"tools":{}},"serverInfo":{"name":"fake-agent","version":"0.0.0"}}`,
			mcp.LATEST_LEGACY_PROTOCOL_VERSION))
	case string(mcp.MethodToolsCall):
		f.toolCalls++
		if f.terminateAll || f.terminateOnce {
			f.terminateOnce = false
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeRPCResult(w, req.ID, `{"content":[{"type":"text","text":"ok"}]}`)
	default:
		// notifications/initialized and anything else we don't model.
		w.WriteHeader(http.StatusAccepted)
	}
}

func (f *fakeAgent) counts() (initializes, toolCalls int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.initializes, f.toolCalls
}

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":%s}`, rpcID(id), result)
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}`, rpcID(id))
}

func rpcID(id json.RawMessage) string {
	if len(id) == 0 {
		return "null"
	}
	return string(id)
}

func startFakeAgent(t *testing.T, agent *fakeAgent) string {
	t.Helper()
	srv := httptest.NewServer(agent)
	t.Cleanup(srv.Close)
	return srv.URL + "/mcp"
}

// resultText extracts the first text block of a tool result.
func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("expected content in tool result")
	}
	text, ok := mcp.AsTextContent(res.Content[0])
	if !ok {
		t.Fatalf("expected text content, got %T", res.Content[0])
	}
	return text.Text
}

func TestIsStaleSession(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{
			// How a terminated session reaches us through mcp-go: the
			// transport wraps the sentinel, the client wraps the transport.
			name: "wrapped session terminated",
			err:  transport.NewError(fmt.Errorf("failed to send request: %w", transport.ErrSessionTerminated)),
			want: true,
		},
		{
			name: "other transport error",
			err:  transport.NewError(errors.New("connection refused")),
			want: false,
		},
		{"unrelated error", errors.New("tool not found"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isStaleSession(tt.err); got != tt.want {
				t.Errorf("isStaleSession(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestEvictSessionOnlyDropsCallersOwnClient(t *testing.T) {
	c := New("test")
	defer c.Close()

	mine, err := newTestSession()
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}
	replacement, err := newTestSession()
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}

	c.sessions["agent"] = replacement
	c.evictSession("agent", mine)
	if got := c.sessions["agent"]; got != replacement {
		t.Errorf("expected replacement session to survive eviction, got %v", got)
	}

	c.evictSession("agent", replacement)
	if _, ok := c.sessions["agent"]; ok {
		t.Error("expected matching session to be evicted")
	}
}

func TestCallToolRecoversFromStaleCachedSession(t *testing.T) {
	agent := &fakeAgent{}
	baseURL := startFakeAgent(t, agent)

	c := New("test")
	defer c.Close()
	ctx := context.Background()

	// Prime the cache with a working session.
	res, err := c.Status(ctx, "agent", baseURL)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if got := resultText(t, res); got != "ok" {
		t.Errorf("first call returned %q, want %q", got, "ok")
	}
	if got := c.SessionID("agent"); got != "session-1" {
		t.Errorf("cached session ID = %q, want %q", got, "session-1")
	}

	// The agent restarts: the next call on the cached session is rejected.
	agent.mu.Lock()
	agent.terminateOnce = true
	agent.mu.Unlock()

	res, err = c.Status(ctx, "agent", baseURL)
	if err != nil {
		t.Fatalf("call after agent restart: %v", err)
	}
	if got := resultText(t, res); got != "ok" {
		t.Errorf("recovered call returned %q, want %q", got, "ok")
	}

	initializes, toolCalls := agent.counts()
	if initializes != 2 {
		t.Errorf("initializes = %d, want 2 (one re-initialization)", initializes)
	}
	if toolCalls != 3 {
		t.Errorf("tools/call requests = %d, want 3 (success, rejected, retry)", toolCalls)
	}
	if got := c.SessionID("agent"); got != "session-2" {
		t.Errorf("cached session ID = %q, want %q", got, "session-2")
	}
}

func TestCallToolRetriesStaleSessionOnlyOnce(t *testing.T) {
	agent := &fakeAgent{}
	baseURL := startFakeAgent(t, agent)

	c := New("test")
	defer c.Close()
	ctx := context.Background()

	if _, err := c.Status(ctx, "agent", baseURL); err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Every call from here on is rejected, so the retry cannot succeed.
	agent.mu.Lock()
	agent.terminateAll = true
	agent.mu.Unlock()

	if _, err := c.Status(ctx, "agent", baseURL); err == nil {
		t.Fatal("expected error when the session stays terminated")
	}

	initializes, toolCalls := agent.counts()
	if initializes != 2 {
		t.Errorf("initializes = %d, want 2 (exactly one retry)", initializes)
	}
	if toolCalls != 3 {
		t.Errorf("tools/call requests = %d, want 3 (success, rejected, rejected)", toolCalls)
	}
	if _, ok := c.sessions["agent"]; ok {
		t.Error("expected the terminated session to be evicted")
	}
}

func TestCallToolDoesNotRetryFreshSession(t *testing.T) {
	agent := &fakeAgent{terminateAll: true}
	baseURL := startFakeAgent(t, agent)

	c := New("test")
	defer c.Close()

	_, err := c.Status(context.Background(), "agent", baseURL)
	if err == nil {
		t.Fatal("expected error from terminated session")
	}
	if !errors.Is(err, transport.ErrSessionTerminated) {
		t.Errorf("expected a session-terminated error, got %v", err)
	}

	initializes, toolCalls := agent.counts()
	if initializes != 1 {
		t.Errorf("initializes = %d, want 1 (a just-initialized session is not retried)", initializes)
	}
	if toolCalls != 1 {
		t.Errorf("tools/call requests = %d, want 1", toolCalls)
	}
	if _, ok := c.sessions["agent"]; ok {
		t.Error("expected no cached session after failure")
	}
}

// newTestSession builds an unstarted MCP client, enough to stand in as a
// cache entry identity in tests.
func newTestSession() (*mcpclient.Client, error) {
	return mcpclient.NewStreamableHttpClient("http://127.0.0.1:1/mcp")
}

// TestCallToolOnModernConnectionReusesSession covers the protocol version
// that motivated dropping the probe: 2026-07-28 holds no server-side session,
// so a cached client can be reused with no extra round-trip and nothing to
// keep alive.
func TestCallToolOnModernConnectionReusesSession(t *testing.T) {
	agent := &fakeAgent{modern: true}
	baseURL := startFakeAgent(t, agent)

	c := New("test")
	defer c.Close()
	ctx := context.Background()

	for i, call := range []func() (*mcp.CallToolResult, error){
		func() (*mcp.CallToolResult, error) { return c.Status(ctx, "agent", baseURL) },
		func() (*mcp.CallToolResult, error) { return c.Status(ctx, "agent", baseURL) },
	} {
		res, err := call()
		if err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
		if got := resultText(t, res); got != "ok" {
			t.Errorf("call %d returned %q, want %q", i+1, got, "ok")
		}
	}

	agent.mu.Lock()
	defer agent.mu.Unlock()

	if agent.discovers != 1 {
		t.Errorf("server/discover requests = %d, want 1 (the cached session is reused as is)", agent.discovers)
	}
	if agent.initializes != 0 {
		t.Errorf("initializes = %d, want 0 (a modern server never sees the handshake)", agent.initializes)
	}
	if agent.toolCalls != 2 {
		t.Errorf("tools/call requests = %d, want 2", agent.toolCalls)
	}
	if agent.sessionHeaderSeen {
		t.Error("modern connections must not send Mcp-Session-Id")
	}
	if got := c.SessionID("agent"); got != "" {
		t.Errorf("session ID = %q, want empty on a stateless connection", got)
	}
}
