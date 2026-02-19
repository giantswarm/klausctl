// Package mcpclient provides a lightweight MCP HTTP client for communicating
// with klaus agent instances. It wraps the mcp-go client library to handle
// session initialization, tool invocation, and per-instance session caching.
package mcpclient

import (
	"context"
	"fmt"
	"sync"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// session tracks a cached MCP client connection.
type session struct {
	client    *mcpclient.Client
	createdAt time.Time
}

// Client manages MCP connections to klaus agent instances. It caches sessions
// per instance to avoid re-initializing on every call.
type Client struct {
	mu       sync.Mutex
	sessions map[string]*session
}

// New creates a new mcpclient.Client.
func New() *Client {
	return &Client{
		sessions: make(map[string]*session),
	}
}

// getOrCreateSession returns a cached MCP client for the given instance or
// creates a new one. The baseURL should be the agent's MCP endpoint
// (e.g. http://localhost:8080/mcp).
func (c *Client) getOrCreateSession(ctx context.Context, instanceName, baseURL string) (*mcpclient.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if s, ok := c.sessions[instanceName]; ok {
		if err := s.client.Ping(ctx); err == nil {
			return s.client, nil
		}
		_ = s.client.Close()
		delete(c.sessions, instanceName)
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
		Version: "1.0.0",
	}
	if _, err := mc.Initialize(ctx, initReq); err != nil {
		_ = mc.Close()
		return nil, fmt.Errorf("initializing MCP session for %s: %w", baseURL, err)
	}

	c.sessions[instanceName] = &session{
		client:    mc,
		createdAt: time.Now(),
	}

	return mc, nil
}

// callTool invokes a named tool on the agent instance and returns the raw text result.
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

	if s, ok := c.sessions[instanceName]; ok {
		return s.client.GetSessionId()
	}
	return ""
}

// invalidateSession removes a cached session.
func (c *Client) invalidateSession(instanceName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if s, ok := c.sessions[instanceName]; ok {
		_ = s.client.Close()
		delete(c.sessions, instanceName)
	}
}

// Close closes all cached sessions.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for name, s := range c.sessions {
		_ = s.client.Close()
		delete(c.sessions, name)
	}
}
