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
	"github.com/giantswarm/klausctl/pkg/mcpserverstore"
	"github.com/giantswarm/klausctl/pkg/orchestrator"
	"github.com/giantswarm/klausctl/pkg/secret"
)

// RegisterTools registers all artifact discovery tools on the MCP server.
func RegisterTools(s *mcpserver.MCPServer, sc *server.ServerContext) {
	registerToolchainList(s, sc)
	registerPersonalityList(s, sc)
	registerPluginList(s, sc)
	registerSecretList(s, sc)
	registerMcpServerAdd(s, sc)
	registerMcpServerList(s, sc)
	registerMcpServerRemove(s, sc)
	registerSourceList(s, sc)
	registerSourceAdd(s, sc)
	registerSourceRemove(s, sc)
}

func registerToolchainList(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_toolchain_list",
		mcp.WithDescription("List available toolchain images as JSON"),
		mcp.WithBoolean("remote", mcp.Description("List from remote registry instead of local cache (default: false)")),
		mcp.WithString("source", mcp.Description("Filter to a specific source name")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleToolchainList(ctx, req, sc)
	})
}

func registerPersonalityList(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_personality_list",
		mcp.WithDescription("List available personalities as JSON"),
		mcp.WithBoolean("remote", mcp.Description("List from remote registry instead of local cache (default: false)")),
		mcp.WithString("source", mcp.Description("Filter to a specific source name")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handlePersonalityList(ctx, req, sc)
	})
}

func registerPluginList(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_plugin_list",
		mcp.WithDescription("List available plugins as JSON"),
		mcp.WithBoolean("remote", mcp.Description("List from remote registry instead of local cache (default: false)")),
		mcp.WithString("source", mcp.Description("Filter to a specific source name")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handlePluginList(ctx, req, sc)
	})
}

// resolverFromRequest builds a SourceResolver, optionally filtered to a single source.
func resolverFromRequest(req mcp.CallToolRequest, sc *server.ServerContext) (*config.SourceResolver, error) {
	resolver := sc.SourceResolver()
	sourceFilter := req.GetString("source", "")
	if sourceFilter != "" {
		return resolver.ForSource(sourceFilter)
	}
	return resolver, nil
}

// --- Handlers ---

type toolchainEntry struct {
	Name       string `json:"name"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Size       string `json:"size,omitempty"`
}

func handleToolchainList(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	remote := req.GetBool("remote", false)
	resolver, err := resolverFromRequest(req, sc)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if remote {
		return toolchainListRemote(ctx, resolver)
	}

	return toolchainListLocal(ctx, sc, resolver)
}

func toolchainListLocal(ctx context.Context, sc *server.ServerContext, resolver *config.SourceResolver) (*mcp.CallToolResult, error) {
	rt, err := sc.DetectRuntime(&config.Config{})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("detecting runtime: %v", err)), nil
	}

	all, err := rt.Images(ctx, "")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("listing images: %v", err)), nil
	}

	registries := resolver.ToolchainRegistries()
	var entries []toolchainEntry
	for _, img := range all {
		for _, sr := range registries {
			if strings.HasPrefix(img.Repository, sr.Registry+"/") {
				entries = append(entries, toolchainEntry{
					Name:       klausoci.ShortName(img.Repository),
					Repository: img.Repository,
					Tag:        img.Tag,
					Size:       img.Size,
				})
				break
			}
		}
	}

	return server.JSONResult(entries)
}

func toolchainListRemote(ctx context.Context, resolver *config.SourceResolver) (*mcp.CallToolResult, error) {
	entries, err := listRemoteFromRegistries(ctx, resolver.ToolchainRegistries(), "toolchains")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return server.JSONResult(entries)
}

func handlePersonalityList(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	remote := req.GetBool("remote", false)
	resolver, err := resolverFromRequest(req, sc)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if remote {
		entries, err := listRemoteFromRegistries(ctx, resolver.PersonalityRegistries(), "personalities")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
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
	resolver, err := resolverFromRequest(req, sc)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if remote {
		entries, err := listRemoteFromRegistries(ctx, resolver.PluginRegistries(), "plugins")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
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

// listRemoteFromRegistries aggregates remote artifacts from multiple source registries.
func listRemoteFromRegistries(ctx context.Context, registries []config.SourceRegistry, artifactType string) ([]remoteArtifactEntry, error) {
	var all []remoteArtifactEntry
	for _, sr := range registries {
		entries, err := listLatestRemote(ctx, sr.Registry, nil)
		if err != nil {
			return nil, fmt.Errorf("listing remote %s: %w", artifactType, err)
		}
		all = append(all, entries...)
	}
	return all, nil
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

// --- Secret and MCP server tools ---

func registerSecretList(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_secret_list",
		mcp.WithDescription("List stored secret names (values are never exposed)"),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleSecretList(ctx, req, sc)
	})
}

func handleSecretList(_ context.Context, _ mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	store, err := secret.Load(sc.Paths.SecretsFile)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading secrets: %v", err)), nil
	}
	return server.JSONResult(store.List())
}

func registerMcpServerAdd(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_mcpserver_add",
		mcp.WithDescription("Add a managed MCP server definition (name, url, optional secret reference)"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Server name")),
		mcp.WithString("url", mcp.Required(), mcp.Description("MCP server URL")),
		mcp.WithString("secret", mcp.Description("Secret name for Bearer token authentication")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleMcpServerAdd(ctx, req, sc)
	})
}

func handleMcpServerAdd(_ context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	url, err := req.RequireString("url")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	secretName := req.GetString("secret", "")

	store, err := mcpserverstore.Load(sc.Paths.McpServersFile)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading MCP servers: %v", err)), nil
	}

	store.Add(name, mcpserverstore.McpServerDef{
		URL:    url,
		Secret: secretName,
	})

	if err := store.Save(); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("saving MCP servers: %v", err)), nil
	}

	return server.JSONResult(map[string]string{
		"name":   name,
		"status": "added",
	})
}

func registerMcpServerList(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_mcpserver_list",
		mcp.WithDescription("List managed MCP server names and URLs (secret values are never exposed)"),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleMcpServerList(ctx, req, sc)
	})
}

type mcpServerEntry struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Secret string `json:"secret,omitempty"`
}

func handleMcpServerList(_ context.Context, _ mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	store, err := mcpserverstore.Load(sc.Paths.McpServersFile)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading MCP servers: %v", err)), nil
	}

	names := store.List()
	all := store.All()
	entries := make([]mcpServerEntry, 0, len(names))
	for _, name := range names {
		def := all[name]
		entries = append(entries, mcpServerEntry{
			Name:   name,
			URL:    def.URL,
			Secret: def.Secret,
		})
	}

	return server.JSONResult(entries)
}

func registerMcpServerRemove(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_mcpserver_remove",
		mcp.WithDescription("Remove a managed MCP server by name"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Server name")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleMcpServerRemove(ctx, req, sc)
	})
}

func handleMcpServerRemove(_ context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	store, err := mcpserverstore.Load(sc.Paths.McpServersFile)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading MCP servers: %v", err)), nil
	}

	if err := store.Remove(name); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := store.Save(); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("saving MCP servers: %v", err)), nil
	}

	return server.JSONResult(map[string]string{
		"name":   name,
		"status": "removed",
	})
}

// --- Source tools ---

func registerSourceList(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_source_list",
		mcp.WithDescription("List configured artifact sources as JSON"),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleSourceList(ctx, req, sc)
	})
}

type sourceEntry struct {
	Name     string `json:"name"`
	Registry string `json:"registry"`
	Default  bool   `json:"default,omitempty"`
}

func handleSourceList(_ context.Context, _ mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	cfg, err := config.LoadSourceConfig(sc.Paths.SourcesFile)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading sources: %v", err)), nil
	}
	entries := make([]sourceEntry, len(cfg.Sources))
	for i, s := range cfg.Sources {
		entries[i] = sourceEntry{
			Name:     s.Name,
			Registry: s.Registry,
			Default:  s.Default,
		}
	}
	return server.JSONResult(entries)
}

func registerSourceAdd(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_source_add",
		mcp.WithDescription("Add a new artifact source (name + registry base URL)"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Source name")),
		mcp.WithString("registry", mcp.Required(), mcp.Description("Registry base URL")),
		mcp.WithString("toolchains", mcp.Description("Override toolchain registry path")),
		mcp.WithString("personalities", mcp.Description("Override personality registry path")),
		mcp.WithString("plugins", mcp.Description("Override plugin registry path")),
		mcp.WithBoolean("default", mcp.Description("Set as the default source")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleSourceAdd(ctx, req, sc)
	})
}

func handleSourceAdd(_ context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	registry, err := req.RequireString("registry")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	cfg, err := config.LoadSourceConfig(sc.Paths.SourcesFile)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading sources: %v", err)), nil
	}

	src := config.Source{
		Name:          name,
		Registry:      registry,
		Toolchains:    req.GetString("toolchains", ""),
		Personalities: req.GetString("personalities", ""),
		Plugins:       req.GetString("plugins", ""),
	}

	if err := cfg.Add(src); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if req.GetBool("default", false) {
		if err := cfg.SetDefault(name); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
	}

	if err := config.EnsureDir(sc.Paths.ConfigDir); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("creating config directory: %v", err)), nil
	}

	if err := cfg.SaveTo(sc.Paths.SourcesFile); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("saving sources: %v", err)), nil
	}

	if err := sc.ReloadSourceConfig(); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("reloading sources: %v", err)), nil
	}

	return server.JSONResult(map[string]string{
		"name":   name,
		"status": "added",
	})
}

func registerSourceRemove(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_source_remove",
		mcp.WithDescription("Remove an artifact source by name"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Source name")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleSourceRemove(ctx, req, sc)
	})
}

func handleSourceRemove(_ context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	cfg, err := config.LoadSourceConfig(sc.Paths.SourcesFile)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("loading sources: %v", err)), nil
	}

	if err := cfg.Remove(name); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if err := cfg.SaveTo(sc.Paths.SourcesFile); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("saving sources: %v", err)), nil
	}

	if err := sc.ReloadSourceConfig(); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("reloading sources: %v", err)), nil
	}

	return server.JSONResult(map[string]string{
		"name":   name,
		"status": "removed",
	})
}
