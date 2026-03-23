package instance

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/archive"
)

func registerArchiveList(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_archive_list",
		mcp.WithDescription("List archived instance transcripts with optional filtering and pagination"),
		mcp.WithNumber("limit", mcp.Description("Max entries to return (default: 20)")),
		mcp.WithNumber("offset", mcp.Description("Skip first N matched entries for pagination (default: 0)")),
		mcp.WithString("since", mcp.Description("Only entries stopped after this RFC3339 date")),
		mcp.WithString("name", mcp.Description("Substring match on instance name")),
		mcp.WithBoolean("tagged", mcp.Description("If true, only entries with tags; if false, only entries without tags")),
		mcp.WithString("outcome", mcp.Description("Filter by tags.outcome value (success/partial/failed)")),
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

type archiveListResponse struct {
	Total int                   `json:"total"`
	Items []archive.ListSummary `json:"items"`
}

func handleArchiveList(_ context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	entries, err := archive.LoadAll(sc.Paths.ArchivesDir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading archives: %v", err)), nil
	}

	// Build filter from request parameters.
	var f archive.Filter
	if sinceStr := req.GetString("since", ""); sinceStr != "" {
		t, err := time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid since value: %v", err)), nil
		}
		f.Since = t
	}
	f.Name = req.GetString("name", "")
	f.Outcome = req.GetString("outcome", "")

	args := req.GetArguments()
	if _, ok := args["tagged"]; ok {
		tagged := req.GetBool("tagged", false)
		f.Tagged = &tagged
	}

	entries = archive.FilterEntries(entries, f)
	total := len(entries)

	limit := int(req.GetFloat("limit", 20))
	offset := int(req.GetFloat("offset", 0))

	if offset < 0 {
		offset = 0
	}
	if offset > len(entries) {
		offset = len(entries)
	}
	entries = entries[offset:]
	if limit > 0 && limit < len(entries) {
		entries = entries[:limit]
	}

	items := make([]archive.ListSummary, 0, len(entries))
	for _, e := range entries {
		items = append(items, e.ToListSummary())
	}

	return server.JSONResult(archiveListResponse{Total: total, Items: items})
}

func registerArchiveTag(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_archive_tag",
		mcp.WithDescription("Attach metadata tags to an archived instance transcript"),
		mcp.WithString("uuid", mcp.Required(), mcp.Description("Archive UUID")),
		mcp.WithObject("tags", mcp.Required(), mcp.Description("Key-value pairs to merge into existing tags (overwrites existing keys)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleArchiveTag(ctx, req, sc)
	})
}

func handleArchiveTag(_ context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	uuid, err := req.RequireString("uuid")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	rawTags, err := extractStringMap(req.GetArguments(), "tags")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if len(rawTags) == 0 {
		return mcp.NewToolResultError("tags must be a non-empty object with string values"), nil
	}

	entry, err := archive.Tag(sc.Paths.ArchivesDir, uuid, rawTags)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("tagging archive: %v", err)), nil
	}

	return server.JSONResult(entry)
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
