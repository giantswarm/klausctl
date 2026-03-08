package instance

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/archive"
)

func registerArchiveList(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_archive_list",
		mcp.WithDescription("List all archived instance transcripts"),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleArchiveList(ctx, req, sc)
	})
}

func registerArchiveShow(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_archive_show",
		mcp.WithDescription("Show a single archived instance transcript by UUID"),
		mcp.WithString("uuid", mcp.Required(), mcp.Description("Archive UUID")),
		mcp.WithBoolean("full", mcp.Description("Include the full messages array in the response (default: false)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleArchiveShow(ctx, req, sc)
	})
}

func handleArchiveList(_ context.Context, _ mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	entries, err := archive.LoadAll(sc.Paths.ArchivesDir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading archives: %v", err)), nil
	}

	list := make([]archive.ListSummary, 0, len(entries))
	for _, e := range entries {
		list = append(list, e.ToListSummary())
	}

	return server.JSONResult(list)
}

func handleArchiveShow(_ context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	uuid, err := req.RequireString("uuid")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	full := req.GetBool("full", false)

	entry, err := archive.Load(sc.Paths.ArchivesDir, uuid)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading archive: %v", err)), nil
	}

	if !full {
		entry.Messages = nil
	}

	return server.JSONResult(entry)
}
