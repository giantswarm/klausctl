package instance

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/archive"
)

func registerStatsSummary(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_stats_summary",
		mcp.WithDescription("Aggregate overview of archived runs: totals, outcome breakdown, cost, duration"),
		mcp.WithString("since", mcp.Description("Include entries stopped after this date (YYYY-MM-DD)")),
		mcp.WithString("repo", mcp.Description("Filter by repo tag")),
		mcp.WithString("outcome", mcp.Description("Filter by outcome tag: success, partial, or failed")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleStatsSummary(ctx, req, sc)
	})
}

func registerStatsSpend(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_stats_spend",
		mcp.WithDescription("Cost breakdown grouped by week, repo, or complexity"),
		mcp.WithString("group_by", mcp.Description("Group by: week (default), repo, or complexity")),
		mcp.WithNumber("weeks", mcp.Description("Limit to last N weeks (default: 8, max: 520)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleStatsSpend(ctx, req, sc)
	})
}

func registerStatsTrends(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_stats_trends",
		mcp.WithDescription("Week-over-week trends: runs, success rate, avg cost, avg messages"),
		mcp.WithNumber("weeks", mcp.Description("Limit to last N weeks (default: 8, max: 520)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleStatsTrends(ctx, req, sc)
	})
}

func handleStatsSummary(_ context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	entries, err := archive.LoadAll(sc.Paths.ArchivesDir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading archives: %v", err)), nil
	}

	var filters archive.SummaryFilters

	if since := req.GetString("since", ""); since != "" {
		t, err := time.Parse("2006-01-02", since)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid since date %q: expected YYYY-MM-DD", since)), nil
		}
		filters.Since = t
	}
	filters.Repo = req.GetString("repo", "")

	outcome := req.GetString("outcome", "")
	if outcome != "" {
		switch outcome {
		case "success", "partial", "failed":
		default:
			return mcp.NewToolResultError(fmt.Sprintf("invalid outcome %q: use success, partial, or failed", outcome)), nil
		}
	}
	filters.Outcome = outcome

	stats := archive.ComputeSummary(entries, filters)
	return server.JSONResult(stats)
}

func handleStatsSpend(_ context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	entries, err := archive.LoadAll(sc.Paths.ArchivesDir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading archives: %v", err)), nil
	}

	groupBy := req.GetString("group_by", "week")
	switch groupBy {
	case "week", "repo", "complexity":
	default:
		return mcp.NewToolResultError(fmt.Sprintf("invalid group_by %q: use week, repo, or complexity", groupBy)), nil
	}

	weeks, err := parseWeeks(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	groups := archive.ComputeSpend(entries, groupBy, weeks)
	return server.JSONResult(groups)
}

func handleStatsTrends(_ context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	entries, err := archive.LoadAll(sc.Paths.ArchivesDir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading archives: %v", err)), nil
	}

	weeks, err := parseWeeks(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	trends := archive.ComputeTrends(entries, weeks)
	return server.JSONResult(trends)
}

// parseWeeks extracts and validates the weeks parameter from an MCP request.
func parseWeeks(req mcp.CallToolRequest) (int, error) {
	weeks := int(math.Round(req.GetFloat("weeks", 8)))
	if weeks < 1 || weeks > 520 {
		return 0, fmt.Errorf("weeks must be between 1 and 520")
	}
	return weeks, nil
}
