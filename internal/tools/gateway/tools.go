// Package gateway implements MCP tool handlers for klaus-gateway bridge
// lifecycle management. The tool surface mirrors `klausctl gateway` exactly:
// every CLI flag has an MCP input of the same shape, enforced by a parity
// test in cmd/gateway_parity_test.go.
package gateway

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/klausctl/internal/gatewaysurface"
	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/gatewaybridge"
)

// RegisterTools registers klaus-gateway bridge lifecycle tools on the MCP server.
func RegisterTools(s *mcpserver.MCPServer, sc *server.ServerContext) {
	registerStart(s, sc)
	registerStop(s, sc)
	registerStatus(s, sc)
}

func registerStart(s *mcpserver.MCPServer, sc *server.ServerContext) {
	opts := []mcp.ToolOption{
		mcp.WithDescription("Start the klaus-gateway bridge. Launches the klaus-gateway process (and optionally agentgateway) in the background, writes PID/port files, health-checks /healthz, and auto-registers 'klaus-gateway' in the MCP server store."),
	}
	for _, f := range gatewaysurface.StartFlags {
		opts = append(opts, mcpInputFor(f))
	}
	tool := mcp.NewTool("klaus_gateway_start", opts...)

	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		bridgeOpts := gatewaybridge.Options{
			WithAgentGateway: req.GetBool("withAgentgateway", false),
			Port:             req.GetInt("port", 0),
			AgentGatewayPort: req.GetInt("agentgatewayPort", 0),
			KlausGatewayBin:  req.GetString("klausGatewayBin", ""),
			AgentGatewayBin:  req.GetString("agentgatewayBin", ""),
			LogLevel:         req.GetString("logLevel", ""),
		}

		st, err := gatewaybridge.Start(ctx, sc.Paths, bridgeOpts)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return server.JSONResult(st)
	})
}

func registerStop(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_gateway_stop",
		mcp.WithDescription("Stop the running klaus-gateway bridge (and the agentgateway sidecar if it was started)."),
	)
	s.AddTool(tool, func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := gatewaybridge.Stop(ctx, sc.Paths); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return server.JSONResult(map[string]string{"status": "stopped"})
	})
}

func registerStatus(s *mcpserver.MCPServer, sc *server.ServerContext) {
	opts := []mcp.ToolOption{
		mcp.WithDescription("Check the klaus-gateway bridge status. Returns the klaus-gateway process state (PID, port, URL, health), enabled adapters, and -- when started with agentgateway -- an agentGateway sub-status."),
	}
	for _, f := range gatewaysurface.StatusFlags {
		opts = append(opts, mcpInputFor(f))
	}
	tool := mcp.NewTool("klaus_gateway_status", opts...)

	s.AddTool(tool, func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// The status tool currently returns the same JSON shape regardless of
		// the output value (MCP clients always consume JSON); the flag is
		// accepted solely to preserve parity with the CLI surface.
		_ = req.GetString("output", "")
		st := gatewaybridge.GetStatus(sc.Paths)
		return server.JSONResult(st)
	})
}

func mcpInputFor(f gatewaysurface.Flag) mcp.ToolOption {
	switch f.Kind {
	case "bool":
		return mcp.WithBoolean(f.MCPKey, mcp.Description(f.Description))
	case "int":
		return mcp.WithNumber(f.MCPKey, mcp.Description(f.Description))
	default:
		return mcp.WithString(f.MCPKey, mcp.Description(f.Description))
	}
}
