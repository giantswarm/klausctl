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
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleArchiveShow(ctx, req, sc)
	})
}

type archiveListEntry struct {
	UUID         string   `json:"uuid"`
	Name         string   `json:"name"`
	Status       string   `json:"status"`
	StoppedAt    string   `json:"stopped_at"`
	MessageCount int      `json:"message_count"`
	TotalCostUSD *float64 `json:"total_cost_usd,omitempty"`
}

func handleArchiveList(_ context.Context, _ mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	entries, err := archive.LoadAll(sc.Paths.ArchivesDir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading archives: %v", err)), nil
	}

	list := make([]archiveListEntry, 0, len(entries))
	for _, e := range entries {
		list = append(list, archiveListEntry{
			UUID:         e.UUID,
			Name:         e.Name,
			Status:       e.Status,
			StoppedAt:    e.StoppedAt.Format("2006-01-02T15:04:05Z07:00"),
			MessageCount: e.MessageCount,
			TotalCostUSD: e.TotalCostUSD,
		})
	}

	return server.JSONResult(list)
}

func handleArchiveShow(_ context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	uuid, err := req.RequireString("uuid")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	entry, err := archive.Load(sc.Paths.ArchivesDir, uuid)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading archive: %v", err)), nil
	}

	return server.JSONResult(entry)
}
