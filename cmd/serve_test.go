package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	internalserver "github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/config"
)

func TestServeCommandRegistered(t *testing.T) {
	assertCommandOnRoot(t, "serve")
}

// callTool sends one tools/call through the server's message handler, the
// way a client's request arrives, and returns the tool result.
func callTool(t *testing.T, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	// No docker or podman on PATH: a handler the validation lets through
	// fails at runtime detection instead of starting a container.
	t.Setenv("PATH", "")
	srv := newMCPServer(&internalserver.ServerContext{Paths: &config.Paths{ConfigDir: t.TempDir()}})
	msg, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, ok := srv.HandleMessage(context.Background(), msg).(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("tools/call %s: no result response", tool)
	}
	result, ok := resp.Result.(*mcp.CallToolResult)
	if !ok {
		t.Fatalf("tools/call %s: result is %T", tool, resp.Result)
	}
	return result
}

func resultText(r *mcp.CallToolResult) string {
	var parts []string
	for _, c := range r.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// TestUnknownToolArgumentsAreRejected (giantswarm/klausctl#318): klaus_run
// called with the misspelled secrets/secretEnv of the 2026-08-21 sweep
// answers an error naming both keys instead of starting an instance whose
// agent cannot log in.
func TestUnknownToolArgumentsAreRejected(t *testing.T) {
	r := callTool(t, "klaus_run", map[string]any{
		"name":      "sweep",
		"message":   "hello",
		"secrets":   []any{"anthropic-api-key"},
		"secretEnv": map[string]any{"ANTHROPIC_API_KEY": "anthropic-api-key"},
	})
	if !r.IsError {
		t.Fatalf("an unknown argument must be an error, got %q", resultText(r))
	}
	text := resultText(r)
	for _, key := range []string{`"secrets"`, `"secretEnv" (did you mean "secretEnvVars"?)`} {
		if !strings.Contains(text, key) {
			t.Errorf("the error names the unknown key %q: %q", key, text)
		}
	}
}

// TestKnownToolArgumentsPassValidation: declared arguments reach the handler,
// whose own answer comes back, not a schema error.
func TestKnownToolArgumentsPassValidation(t *testing.T) {
	r := callTool(t, "klaus_status", map[string]any{"name": "no-such-instance"})
	if strings.Contains(resultText(r), "unknown argument") {
		t.Fatalf("a declared argument is not a schema error: %q", resultText(r))
	}
}
