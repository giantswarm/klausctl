// Package artifact implements MCP tool handlers for artifact discovery:
// toolchain list, personality list, and plugin list.
package artifact

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	klausoci "github.com/giantswarm/klaus-oci"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/orchestrator"
)

// RegisterTools registers all artifact discovery tools on the MCP server.
func RegisterTools(s *mcpserver.MCPServer, sc *server.ServerContext) {
	registerToolchainList(s, sc)
	registerPersonalityList(s, sc)
	registerPluginList(s, sc)
}

func registerToolchainList(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_toolchain_list",
		mcp.WithDescription("List available toolchain images as JSON"),
		mcp.WithBoolean("remote", mcp.Description("List from remote registry instead of local cache (default: false)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleToolchainList(ctx, req, sc)
	})
}

func registerPersonalityList(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_personality_list",
		mcp.WithDescription("List available personalities as JSON"),
		mcp.WithBoolean("remote", mcp.Description("List from remote registry instead of local cache (default: false)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handlePersonalityList(ctx, req, sc)
	})
}

func registerPluginList(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_plugin_list",
		mcp.WithDescription("List available plugins as JSON"),
		mcp.WithBoolean("remote", mcp.Description("List from remote registry instead of local cache (default: false)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handlePluginList(ctx, req, sc)
	})
}

// --- Handlers ---

const toolchainImageSubstring = "klaus-"

type toolchainEntry struct {
	Name       string `json:"name"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Size       string `json:"size,omitempty"`
}

func handleToolchainList(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	remote := req.GetBool("remote", false)

	if remote {
		return toolchainListRemote(ctx)
	}

	return toolchainListLocal(ctx, sc)
}

func toolchainListLocal(ctx context.Context, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	rt, err := sc.DetectRuntime(&config.Config{})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("detecting runtime: %v", err)), nil
	}

	all, err := rt.Images(ctx, "")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("listing images: %v", err)), nil
	}

	var entries []toolchainEntry
	for _, img := range all {
		if strings.Contains(img.Repository, toolchainImageSubstring) {
			entries = append(entries, toolchainEntry{
				Name:       klausoci.ShortToolchainName(img.Repository),
				Repository: img.Repository,
				Tag:        img.Tag,
				Size:       img.Size,
			})
		}
	}

	return server.JSONResult(entries)
}

func toolchainListRemote(ctx context.Context) (*mcp.CallToolResult, error) {
	entries, err := listLatestRemote(ctx, klausoci.DefaultToolchainRegistry, &remoteListOptions{
		Filter: func(repo string) bool {
			parts := strings.Split(repo, "/")
			if len(parts) != 3 {
				return false
			}
			return strings.HasPrefix(parts[2], toolchainImageSubstring)
		},
		ShortName: klausoci.ShortToolchainName,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("listing remote toolchains: %v", err)), nil
	}
	return server.JSONResult(entries)
}

func handlePersonalityList(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	remote := req.GetBool("remote", false)

	if remote {
		entries, err := listLatestRemote(ctx, klausoci.DefaultPersonalityRegistry, nil)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("listing remote personalities: %v", err)), nil
		}
		return server.JSONResult(entries)
	}

	artifacts, err := listLocalArtifacts(sc.Paths.PersonalitiesDir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("listing local personalities: %v", err)), nil
	}
	return server.JSONResult(artifacts)
}

func handlePluginList(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	remote := req.GetBool("remote", false)

	if remote {
		entries, err := listLatestRemote(ctx, klausoci.DefaultPluginRegistry, nil)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("listing remote plugins: %v", err)), nil
		}
		return server.JSONResult(entries)
	}

	artifacts, err := listLocalArtifacts(sc.Paths.PluginsDir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("listing local plugins: %v", err)), nil
	}
	return server.JSONResult(artifacts)
}

// --- Shared helpers ---

type cachedArtifact struct {
	Name   string `json:"name"`
	Ref    string `json:"ref"`
	Digest string `json:"digest"`
}

func listLocalArtifacts(cacheDir string) ([]cachedArtifact, error) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []cachedArtifact{}, nil
		}
		return nil, fmt.Errorf("reading cache directory: %w", err)
	}

	var artifacts []cachedArtifact
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(cacheDir, entry.Name())
		cache, err := klausoci.ReadCacheEntry(dir)
		if err != nil {
			continue
		}
		artifacts = append(artifacts, cachedArtifact{
			Name:   entry.Name(),
			Ref:    cache.Ref,
			Digest: cache.Digest,
		})
	}

	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].Name < artifacts[j].Name
	})
	return artifacts, nil
}

type remoteArtifactEntry struct {
	Name string `json:"name"`
	Ref  string `json:"ref"`
}

type remoteListOptions struct {
	Filter    func(repo string) bool
	ShortName func(repo string) string
}

// listLatestRemote discovers repositories from the registry, resolves the
// latest semver tag for each, and returns a sorted list. Uses the high-level
// ListArtifacts API for concurrent resolution.
func listLatestRemote(ctx context.Context, registryBase string, opts *remoteListOptions) ([]remoteArtifactEntry, error) {
	client := orchestrator.NewDefaultClient()

	var listOpts []klausoci.ListOption
	if opts != nil && opts.Filter != nil {
		listOpts = append(listOpts, klausoci.WithFilter(opts.Filter))
	}

	artifacts, err := client.ListArtifacts(ctx, registryBase, listOpts...)
	if err != nil {
		return nil, fmt.Errorf("discovering remote repositories: %w", err)
	}

	shortNameFn := klausoci.ShortName
	if opts != nil && opts.ShortName != nil {
		shortNameFn = opts.ShortName
	}

	var entries []remoteArtifactEntry
	for _, a := range artifacts {
		entries = append(entries, remoteArtifactEntry{
			Name: shortNameFn(a.Repository),
			Ref:  a.Reference,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}
