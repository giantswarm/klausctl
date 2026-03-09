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
		mcp.WithString("repo", mcp.Description("Filter by repo tag")),
		mcp.WithString("outcome", mcp.Description("Filter by outcome tag: success, partial, or failed")),
		mcp.WithString("complexity", mcp.Description("Filter by complexity tag")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleStatsSpend(ctx, req, sc)
	})
}

func registerStatsTrends(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_stats_trends",
		mcp.WithDescription("Week-over-week trends: runs, success rate, first-attempt rate, avg cost, total cost, avg messages, avg duration, avg complexity"),
		mcp.WithNumber("weeks", mcp.Description("Limit to last N weeks (default: 8, max: 520)")),
		mcp.WithString("repo", mcp.Description("Filter by repo tag")),
		mcp.WithString("outcome", mcp.Description("Filter by outcome tag: success, partial, or failed")),
		mcp.WithString("complexity", mcp.Description("Filter by complexity tag")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleStatsTrends(ctx, req, sc)
	})
}

func registerStatsList(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_stats_list",
		mcp.WithDescription("Tabular view of individual archive entries with filtering and sorting"),
		mcp.WithString("repo", mcp.Description("Filter by repo tag")),
		mcp.WithString("outcome", mcp.Description("Filter by outcome tag: success, partial, or failed")),
		mcp.WithString("complexity", mcp.Description("Filter by complexity tag")),
		mcp.WithString("sort_by", mcp.Description("Sort by: date (default), cost, messages, or duration")),
		mcp.WithNumber("limit", mcp.Description("Limit number of rows (0 = all)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleStatsList(ctx, req, sc)
	})
}

func registerStatsTop(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_stats_top",
		mcp.WithDescription("Show outlier runs sorted by cost, messages, or duration"),
		mcp.WithString("sort_by", mcp.Description("Sort by: cost (default), messages, or duration")),
		mcp.WithNumber("limit", mcp.Description("Number of top entries (default: 10)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleStatsTop(ctx, req, sc)
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

	filters := archive.SummaryFilters{
		Repo:       req.GetString("repo", ""),
		Outcome:    req.GetString("outcome", ""),
		Complexity: req.GetString("complexity", ""),
	}

	groups := archive.ComputeSpend(entries, groupBy, weeks, filters)
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

	filters := archive.SummaryFilters{
		Repo:       req.GetString("repo", ""),
		Outcome:    req.GetString("outcome", ""),
		Complexity: req.GetString("complexity", ""),
	}

	trends := archive.ComputeTrends(entries, weeks, filters)
	return server.JSONResult(trends)
}

func handleStatsList(_ context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	entries, err := archive.LoadAll(sc.Paths.ArchivesDir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading archives: %v", err)), nil
	}

	sortBy := req.GetString("sort_by", "date")
	switch sortBy {
	case "date", "cost", "messages", "duration":
	default:
		return mcp.NewToolResultError(fmt.Sprintf("invalid sort_by %q: use date, cost, messages, or duration", sortBy)), nil
	}

	limit := int(math.Round(req.GetFloat("limit", 0)))
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}

	filters := archive.SummaryFilters{
		Repo:       req.GetString("repo", ""),
		Outcome:    req.GetString("outcome", ""),
		Complexity: req.GetString("complexity", ""),
	}

	list := archive.ComputeList(entries, filters, sortBy, limit)
	return server.JSONResult(list)
}

func handleStatsTop(_ context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	entries, err := archive.LoadAll(sc.Paths.ArchivesDir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading archives: %v", err)), nil
	}

	sortBy := req.GetString("sort_by", "cost")
	switch sortBy {
	case "cost", "messages", "duration":
	default:
		return mcp.NewToolResultError(fmt.Sprintf("invalid sort_by %q: use cost, messages, or duration", sortBy)), nil
	}

	limit := int(math.Round(req.GetFloat("limit", 10)))
	if limit < 1 {
		limit = 10
	}

	list := archive.ComputeList(entries, archive.SummaryFilters{}, sortBy, limit)
	return server.JSONResult(list)
}

// parseWeeks extracts and validates the weeks parameter from an MCP request.
func parseWeeks(req mcp.CallToolRequest) (int, error) {
	weeks := int(math.Round(req.GetFloat("weeks", 8)))
	if weeks < 1 || weeks > 520 {
		return 0, fmt.Errorf("weeks must be between 1 and 520")
	}
	return weeks, nil
}
