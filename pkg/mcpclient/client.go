// Package mcpclient provides a lightweight MCP HTTP client for communicating
// with klaus agent instances. It wraps the mcp-go client library to handle
// session initialization, tool invocation, and per-instance session caching.
package mcpclient

import (
	"context"
	"errors"
	"fmt"
	"sync"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// Client manages MCP connections to klaus agent instances. It caches sessions
// per instance to avoid re-initializing on every call.
type Client struct {
	mu       sync.Mutex
	sessions map[string]*mcpclient.Client
	version  string
	// headers, when non-empty, are applied to every outgoing MCP HTTP
	// request (set via NewWithHeaders for the remote-gateway path).
	headers map[string]string
}

// New creates a new Client. The version string is sent during MCP session
// initialization so the remote agent knows which klausctl build is calling.
func New(version string) *Client {
	return &Client{
		sessions: make(map[string]*mcpclient.Client),
		version:  version,
	}
}

// NewWithHeaders returns a Client that attaches the given HTTP headers to
// every MCP HTTP request. Use this for remote klaus-gateway sessions that
// need the X-Klaus-* routing headers and/or Authorization bearer.
func NewWithHeaders(version string, headers map[string]string) *Client {
	copied := make(map[string]string, len(headers))
	for k, v := range headers {
		copied[k] = v
	}
	return &Client{
		sessions: make(map[string]*mcpclient.Client),
		version:  version,
		headers:  copied,
	}
}

// getOrCreateSession returns a cached MCP client for the given instance or
// creates a new one. The returned bool reports whether the client came from
// the cache, so callers can tell a possibly-stale session apart from one that
// was just initialized.
//
// There is deliberately no liveness probe here. The ping RPC was removed in
// protocol version 2026-07-28 (SEP-2575) and mcp-go's Client.Ping returns nil
// without sending anything on a modern connection, so a pre-flight probe can
// no longer prove a cached session is usable. Staleness is detected on the
// next real request instead, which callTool recovers from.
//
// Network I/O (connect, initialize) happens outside the lock so concurrent
// callers targeting different instances aren't blocked.
func (c *Client) getOrCreateSession(ctx context.Context, instanceName, baseURL string) (*mcpclient.Client, bool, error) {
	c.mu.Lock()
	cached, ok := c.sessions[instanceName]
	c.mu.Unlock()

	if ok {
		return cached, true, nil
	}

	var transportOpts []transport.StreamableHTTPCOption
	if len(c.headers) > 0 {
		transportOpts = append(transportOpts, transport.WithHTTPHeaders(c.headers))
	}

	mc, err := mcpclient.NewStreamableHttpClient(baseURL, transportOpts...)
	if err != nil {
		return nil, false, fmt.Errorf("creating MCP client for %s: %w", baseURL, err)
	}

	if err := mc.Start(ctx); err != nil {
		return nil, false, fmt.Errorf("starting MCP transport for %s: %w", baseURL, err)
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "klausctl",
		Version: c.version,
	}
	if _, err := mc.Initialize(ctx, initReq); err != nil {
		_ = mc.Close()
		return nil, false, fmt.Errorf("initializing MCP session for %s: %w", baseURL, err)
	}

	c.mu.Lock()
	existing, raced := c.sessions[instanceName]
	if !raced {
		c.sessions[instanceName] = mc
	}
	c.mu.Unlock()

	if raced {
		// A concurrent caller initialized first; keep its session and drop
		// ours. Close is deliberately outside the lock, see evictSession.
		_ = mc.Close()
		return existing, true, nil
	}

	return mc, false, nil
}

// isStaleSession reports whether err means the server no longer recognizes
// our session (it answered 404). The request was then definitely not
// executed, and re-initializing is the only way forward. Stateless
// connections (protocol 2026-07-28 and later) hold no server-side session, so
// this only arises against a legacy server that restarted or expired ours.
func isStaleSession(err error) bool {
	return errors.Is(err, transport.ErrSessionTerminated)
}

// callTool invokes a named tool on the agent instance. When a cached session
// turns out to be stale, it is evicted and the call retried once on a freshly
// initialized session. The retry is safe even for side-effecting tools such
// as prompt, because a terminated session means the server never ran the tool.
func (c *Client) callTool(ctx context.Context, instanceName, baseURL, toolName string, args map[string]any) (*mcp.CallToolResult, error) {
	req := mcp.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = args

	for attempt := 0; ; attempt++ {
		mc, fromCache, err := c.getOrCreateSession(ctx, instanceName, baseURL)
		if err != nil {
			return nil, err
		}

		result, err := mc.CallTool(ctx, req)
		if err == nil {
			return result, nil
		}

		c.evictSession(instanceName, mc)

		if fromCache && attempt == 0 && isStaleSession(err) {
			continue
		}
		return nil, fmt.Errorf("calling tool %q on %s: %w", toolName, instanceName, err)
	}
}

// Prompt sends a prompt message to the agent instance.
func (c *Client) Prompt(ctx context.Context, instanceName, baseURL, message string) (*mcp.CallToolResult, error) {
	return c.callTool(ctx, instanceName, baseURL, "prompt", map[string]any{
		"message": message,
	})
}

// Status queries the agent's current status.
func (c *Client) Status(ctx context.Context, instanceName, baseURL string) (*mcp.CallToolResult, error) {
	return c.callTool(ctx, instanceName, baseURL, "status", nil)
}

// Result retrieves the agent's last result. When full is true, the agent
// returns extended detail (tool_calls, model_usage, token_usage, etc.).
func (c *Client) Result(ctx context.Context, instanceName, baseURL string, full bool) (*mcp.CallToolResult, error) {
	var args map[string]any
	if full {
		args = map[string]any{"full": true}
	}
	return c.callTool(ctx, instanceName, baseURL, "result", args)
}

// MessagesOpts holds optional parameters for the Messages call.
type MessagesOpts struct {
	Offset int
	Types  string
}

// Messages retrieves the agent's conversation messages.
// When opts is non-nil, offset and types are forwarded to the agent.
func (c *Client) Messages(ctx context.Context, instanceName, baseURL string, opts *MessagesOpts) (*mcp.CallToolResult, error) {
	args := map[string]any{}
	if opts != nil {
		if opts.Offset > 0 {
			args["offset"] = opts.Offset
		}
		if opts.Types != "" {
			args["types"] = opts.Types
		}
	}
	return c.callTool(ctx, instanceName, baseURL, "messages", args)
}

// SessionID returns the MCP session ID for the given instance, if any.
func (c *Client) SessionID(instanceName string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if mc, ok := c.sessions[instanceName]; ok {
		return mc.GetSessionId()
	}
	return ""
}

// evictSession closes and drops the cached session for instanceName, but only
// if it is still the one the caller used. Without that check, a caller whose
// session went stale could close a replacement another goroutine had already
// initialized. Close is called outside the lock: on a legacy connection it
// issues a DELETE with a five-second timeout, which would otherwise stall
// every caller, including those targeting other instances.
func (c *Client) evictSession(instanceName string, mc *mcpclient.Client) {
	c.mu.Lock()
	cur, ok := c.sessions[instanceName]
	mine := ok && cur == mc
	if mine {
		delete(c.sessions, instanceName)
	}
	c.mu.Unlock()

	if mine {
		_ = cur.Close()
	}
}

// Close closes all cached sessions.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for name, mc := range c.sessions {
		_ = mc.Close()
		delete(c.sessions, name)
	}
}
