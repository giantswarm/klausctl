// Package workspace implements MCP tool handlers for workspace registry
// management: listing, adding, removing organizations and repos, and
// fetching cached repositories.
package workspace

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/workspace"
)

// RegisterTools registers all workspace management tools on the MCP server.
func RegisterTools(s *mcpserver.MCPServer, sc *server.ServerContext) {
	registerList(s, sc)
	registerAddOrg(s, sc)
	registerAddRepo(s, sc)
	registerRemove(s, sc)
	registerFetch(s, sc)
}

func registerList(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_workspace_list",
		mcp.WithDescription("List registered organizations, repos, and cached clones as JSON"),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleList(ctx, req, sc)
	})
}

func registerAddOrg(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_workspace_add_org",
		mcp.WithDescription("Register a GitHub organization in the workspace registry"),
		mcp.WithString("organization", mcp.Required(), mcp.Description("GitHub organization name")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleAddOrg(ctx, req, sc)
	})
}

func registerAddRepo(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_workspace_add_repo",
		mcp.WithDescription("Register a specific repository in the workspace registry"),
		mcp.WithString("repo", mcp.Required(), mcp.Description("Repository in owner/repo format")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleAddRepo(ctx, req, sc)
	})
}

func registerRemove(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_workspace_remove",
		mcp.WithDescription("Unregister an organization or repository from the workspace registry"),
		mcp.WithString("identifier", mcp.Required(), mcp.Description("Organization name or owner/repo to remove")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleRemove(ctx, req, sc)
	})
}

func registerFetch(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_workspace_fetch",
		mcp.WithDescription("Fetch latest commits from origin for cached repositories"),
		mcp.WithString("repo", mcp.Description("Repository in owner/repo format (omit to fetch all cached repos)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleFetch(ctx, req, sc)
	})
}

// --- Handlers ---

type listResult struct {
	Organizations []string          `json:"organizations"`
	Repos         []string          `json:"repos"`
	Cached        []cachedRepoEntry `json:"cached"`
}

type cachedRepoEntry struct {
	Owner      string `json:"owner"`
	Repo       string `json:"repo"`
	Identifier string `json:"identifier"`
}

func handleList(_ context.Context, _ mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	cfg, err := workspace.Load(sc.Paths.WorkspacesFile)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading workspace config: %v", err)), nil
	}

	cached, err := workspace.ListCached(sc.Paths.ReposDir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("listing cached repos: %v", err)), nil
	}

	repos := make([]string, len(cfg.Repos))
	for i, r := range cfg.Repos {
		repos[i] = r.Name
	}

	cachedEntries := make([]cachedRepoEntry, len(cached))
	for i, c := range cached {
		cachedEntries[i] = cachedRepoEntry{
			Owner:      c.Owner,
			Repo:       c.Repo,
			Identifier: c.Identifier,
		}
	}

	result := listResult{
		Organizations: cfg.Organizations,
		Repos:         repos,
		Cached:        cachedEntries,
	}

	// Ensure empty slices serialize as [] not null.
	if result.Organizations == nil {
		result.Organizations = []string{}
	}
	if result.Repos == nil {
		result.Repos = []string{}
	}
	if result.Cached == nil {
		result.Cached = []cachedRepoEntry{}
	}

	return server.JSONResult(result)
}

func handleAddOrg(_ context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	org := req.GetString("organization", "")
	if org == "" {
		return mcp.NewToolResultError("organization is required"), nil
	}

	cfg, err := workspace.Load(sc.Paths.WorkspacesFile)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading workspace config: %v", err)), nil
	}

	for _, existing := range cfg.Organizations {
		if strings.EqualFold(existing, org) {
			return mcp.NewToolResultText(fmt.Sprintf("Organization %q is already registered.", org)), nil
		}
	}

	cfg.Organizations = append(cfg.Organizations, org)
	if err := workspace.Save(sc.Paths.WorkspacesFile, cfg); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("saving workspace config: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Added organization: %s", org)), nil
}

func handleAddRepo(_ context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	identifier := req.GetString("repo", "")
	if identifier == "" {
		return mcp.NewToolResultError("repo is required"), nil
	}

	if strings.Count(identifier, "/") != 1 {
		return mcp.NewToolResultError(fmt.Sprintf("invalid repo format %q: expected owner/repo", identifier)), nil
	}

	cfg, err := workspace.Load(sc.Paths.WorkspacesFile)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading workspace config: %v", err)), nil
	}

	for _, existing := range cfg.Repos {
		if strings.EqualFold(existing.Name, identifier) {
			return mcp.NewToolResultText(fmt.Sprintf("Repository %q is already registered.", identifier)), nil
		}
	}

	cfg.Repos = append(cfg.Repos, workspace.RepoEntry{Name: identifier})
	if err := workspace.Save(sc.Paths.WorkspacesFile, cfg); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("saving workspace config: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Added repository: %s", identifier)), nil
}

func handleRemove(_ context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	identifier := req.GetString("identifier", "")
	if identifier == "" {
		return mcp.NewToolResultError("identifier is required"), nil
	}

	cfg, err := workspace.Load(sc.Paths.WorkspacesFile)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading workspace config: %v", err)), nil
	}

	if strings.Contains(identifier, "/") {
		for i, r := range cfg.Repos {
			if strings.EqualFold(r.Name, identifier) {
				cfg.Repos = append(cfg.Repos[:i], cfg.Repos[i+1:]...)
				if err := workspace.Save(sc.Paths.WorkspacesFile, cfg); err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("saving workspace config: %v", err)), nil
				}
				return mcp.NewToolResultText(fmt.Sprintf("Removed repository: %s", identifier)), nil
			}
		}
		return mcp.NewToolResultError(fmt.Sprintf("repository %q not found in workspace config", identifier)), nil
	}

	for i, org := range cfg.Organizations {
		if strings.EqualFold(org, identifier) {
			cfg.Organizations = append(cfg.Organizations[:i], cfg.Organizations[i+1:]...)
			if err := workspace.Save(sc.Paths.WorkspacesFile, cfg); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("saving workspace config: %v", err)), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("Removed organization: %s", identifier)), nil
		}
	}
	return mcp.NewToolResultError(fmt.Sprintf("organization %q not found in workspace config", identifier)), nil
}

func handleFetch(_ context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	repoArg := req.GetString("repo", "")

	if repoArg != "" {
		if strings.Count(repoArg, "/") != 1 {
			return mcp.NewToolResultError(fmt.Sprintf("invalid repo format %q: expected owner/repo", repoArg)), nil
		}
		parts := strings.SplitN(repoArg, "/", 2)
		owner, repo := parts[0], parts[1]
		if err := workspace.FetchRepo(sc.Paths.ReposDir, owner, repo); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetching %s/%s: %v", owner, repo, err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Fetched %s/%s", owner, repo)), nil
	}

	cached, err := workspace.ListCached(sc.Paths.ReposDir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("listing cached repos: %v", err)), nil
	}

	if len(cached) == 0 {
		return mcp.NewToolResultText("No cached repos to fetch."), nil
	}

	var fetched []string
	for _, r := range cached {
		if err := workspace.FetchRepo(sc.Paths.ReposDir, r.Owner, r.Repo); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetching %s: %v", r.Identifier, err)), nil
		}
		fetched = append(fetched, r.Identifier)
	}

	return mcp.NewToolResultText(fmt.Sprintf("Fetched %d repos: %s", len(fetched), strings.Join(fetched, ", "))), nil
}
