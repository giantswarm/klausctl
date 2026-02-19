// Package mcpclient provides a lightweight MCP HTTP client for communicating
// with klaus agent instances. It wraps the mcp-go client library to handle
// session initialization, tool invocation, and per-instance session caching.
package mcpclient

import (
	"context"
	"fmt"
	"sync"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// Client manages MCP connections to klaus agent instances. It caches sessions
// per instance to avoid re-initializing on every call.
type Client struct {
	mu       sync.Mutex
	sessions map[string]*mcpclient.Client
	version  string
}

// New creates a new Client. The version string is sent during MCP session
// initialization so the remote agent knows which klausctl build is calling.
func New(version string) *Client {
	return &Client{
		sessions: make(map[string]*mcpclient.Client),
		version:  version,
	}
}

// getOrCreateSession returns a cached MCP client for the given instance or
// creates a new one. Network I/O (ping, connect, initialize) happens outside
// the lock so concurrent callers targeting different instances aren't blocked.
func (c *Client) getOrCreateSession(ctx context.Context, instanceName, baseURL string) (*mcpclient.Client, error) {
	c.mu.Lock()
	cached, ok := c.sessions[instanceName]
	c.mu.Unlock()

	if ok {
		if err := cached.Ping(ctx); err == nil {
			return cached, nil
		}
		c.mu.Lock()
		if cur, ok := c.sessions[instanceName]; ok && cur == cached {
			_ = cached.Close()
			delete(c.sessions, instanceName)
		}
		c.mu.Unlock()
	}

	mc, err := mcpclient.NewStreamableHttpClient(baseURL)
	if err != nil {
		return nil, fmt.Errorf("creating MCP client for %s: %w", baseURL, err)
	}

	if err := mc.Start(ctx); err != nil {
		return nil, fmt.Errorf("starting MCP transport for %s: %w", baseURL, err)
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "klausctl",
		Version: c.version,
	}
	if _, err := mc.Initialize(ctx, initReq); err != nil {
		_ = mc.Close()
		return nil, fmt.Errorf("initializing MCP session for %s: %w", baseURL, err)
	}

	c.mu.Lock()
	if existing, ok := c.sessions[instanceName]; ok {
		_ = mc.Close()
		c.mu.Unlock()
		return existing, nil
	}
	c.sessions[instanceName] = mc
	c.mu.Unlock()

	return mc, nil
}

// callTool invokes a named tool on the agent instance.
func (c *Client) callTool(ctx context.Context, instanceName, baseURL, toolName string, args map[string]any) (*mcp.CallToolResult, error) {
	mc, err := c.getOrCreateSession(ctx, instanceName, baseURL)
	if err != nil {
		return nil, err
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = args

	result, err := mc.CallTool(ctx, req)
	if err != nil {
		c.invalidateSession(instanceName)
		return nil, fmt.Errorf("calling tool %q on %s: %w", toolName, instanceName, err)
	}

	return result, nil
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

// Result retrieves the agent's last result.
func (c *Client) Result(ctx context.Context, instanceName, baseURL string) (*mcp.CallToolResult, error) {
	return c.callTool(ctx, instanceName, baseURL, "result", nil)
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

// invalidateSession removes a cached session.
func (c *Client) invalidateSession(instanceName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if mc, ok := c.sessions[instanceName]; ok {
		_ = mc.Close()
		delete(c.sessions, instanceName)
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
