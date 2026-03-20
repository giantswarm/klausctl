// Package muster implements MCP tool handlers for muster bridge lifecycle
// management (start, stop, status).
package muster

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/musterbridge"
)

// RegisterTools registers muster bridge lifecycle tools on the MCP server.
func RegisterTools(s *mcpserver.MCPServer, sc *server.ServerContext) {
	registerStart(s, sc)
	registerStop(s, sc)
	registerStatus(s, sc)
}

func registerStart(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_muster_start",
		mcp.WithDescription("Start the muster MCP bridge. Launches a background muster process that aggregates stdio MCP servers behind an HTTP endpoint for container access. The bridge is auto-registered as 'muster' in the MCP server store."),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		st, err := musterbridge.Start(ctx, sc.Paths)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return server.JSONResult(st)
	})
}

func registerStop(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_muster_stop",
		mcp.WithDescription("Stop the running muster MCP bridge."),
	)
	s.AddTool(tool, func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := musterbridge.Stop(sc.Paths); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return server.JSONResult(map[string]string{"status": "stopped"})
	})
}

func registerStatus(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_muster_status",
		mcp.WithDescription("Check the status of the muster MCP bridge. Returns whether it is running, the PID, port, URL, and which MCP servers are configured."),
	)
	s.AddTool(tool, func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		st := musterbridge.GetStatus(sc.Paths)
		return server.JSONResult(st)
	})
}
