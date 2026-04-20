// Package cache implements MCP tool handlers that expose klausctl's
// on-disk OCI cache (info, prune, refresh).
package cache

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/ocicache"
)

// RegisterTools registers the cache-management tools on the MCP server.
func RegisterTools(s *mcpserver.MCPServer, _ *server.ServerContext) {
	registerInfo(s)
	registerPrune(s)
	registerRefresh(s)
}

func registerInfo(s *mcpserver.MCPServer) {
	tool := mcp.NewTool("klausctl_cache_info",
		mcp.WithDescription("Report the location, size, and per-layer stats of the persistent klaus-oci cache"),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleInfo(ctx, req)
	})
}

func registerPrune(s *mcpserver.MCPServer) {
	tool := mcp.NewTool("klausctl_cache_prune",
		mcp.WithDescription("Remove stale cache entries (or everything with all=true)"),
		mcp.WithBoolean("all", mcp.Description("Wipe the entire cache instead of only stale entries (default: false)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handlePrune(ctx, req)
	})
}

func registerRefresh(s *mcpserver.MCPServer) {
	tool := mcp.NewTool("klausctl_cache_refresh",
		mcp.WithDescription("Force revalidation of cached entries so the next client call refetches from the registry"),
		mcp.WithString("registry", mcp.Description("Limit refresh to a registry base (host or host/prefix)")),
		mcp.WithString("repo", mcp.Description("Limit refresh to a single repository (host/name)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleRefresh(ctx, req)
	})
}

func handleInfo(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	info, err := ocicache.Stat()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("stat cache: %v", err)), nil
	}
	return server.JSONResult(info)
}

func handlePrune(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	all := req.GetBool("all", false)
	res, err := ocicache.Prune(ocicache.PruneOptions{All: all})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("prune cache: %v", err)), nil
	}
	return server.JSONResult(res)
}

func handleRefresh(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	registry := req.GetString("registry", "")
	repo := req.GetString("repo", "")
	if registry != "" && repo != "" {
		return mcp.NewToolResultError("registry and repo are mutually exclusive"), nil
	}
	res, err := ocicache.Refresh(ctx, ocicache.RefreshOptions{
		Registry: registry,
		Repo:     repo,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("refresh cache: %v", err)), nil
	}
	return server.JSONResult(res)
}
