package instance

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/orchestrator"
	"github.com/giantswarm/klausctl/pkg/worktree"
)

// mcpCreateParams holds the parsed parameters for creating an instance via MCP.
type mcpCreateParams struct {
	name           string
	generateSuffix bool
	force          bool
	confirm        bool
	workspace      string
	personality    string
	toolchain      string
	pluginArgs     []string
	sourceFilter   string
	envVars        map[string]string
	mcpServers     map[string]any
	secretEnvVars  map[string]string
	secretFiles    map[string]string
	envForward     []string
	mcpServerRefs  []string
	port           int
	gitAuthorName  string
	gitAuthorEmail string
	gitCredHelper  string
	gitHTTPS       bool
	persistentMode bool
	noIsolate      bool
	noFetch        bool
	permissionMode string
	model          string
	systemPrompt   string
	maxBudgetUSD   *float64
}

// parseMCPCreateParams extracts common create parameters from an MCP request.
func parseMCPCreateParams(req mcp.CallToolRequest) (*mcpCreateParams, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return nil, err
	}
	if err := config.ValidateInstanceName(name); err != nil {
		return nil, err
	}

	workspace := req.GetString("workspace", "")
	if workspace == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("determining current directory: %v", err)
		}
		workspace = cwd
	}

	args := req.GetArguments()
	envVars, err := extractStringMap(args, "envVars")
	if err != nil {
		return nil, err
	}
	mcpServers, err := extractObjectMap(args, "mcpServers")
	if err != nil {
		return nil, err
	}
	secretEnvVars, err := extractStringMap(args, "secretEnvVars")
	if err != nil {
		return nil, err
	}
	secretFiles, err := extractStringMap(args, "secretFiles")
	if err != nil {
		return nil, err
	}

	port := int(req.GetFloat("port", 0))
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("port must be between 1 and 65535, got %d", port)
	}

	gitAuthorName, gitAuthorEmail, err := parseGitAuthor(req.GetString("gitAuthor", ""))
	if err != nil {
		return nil, err
	}

	p := &mcpCreateParams{
		name:           name,
		generateSuffix: req.GetBool("generateSuffix", true),
		force:          req.GetBool("force", false),
		confirm:        req.GetBool("confirm", false),
		workspace:      workspace,
		personality:    req.GetString("personality", ""),
		toolchain:      req.GetString("toolchain", ""),
		pluginArgs:     req.GetStringSlice("plugin", nil),
		sourceFilter:   req.GetString("source", ""),
		envVars:        envVars,
		mcpServers:     mcpServers,
		secretEnvVars:  secretEnvVars,
		secretFiles:    secretFiles,
		envForward:     req.GetStringSlice("envForward", nil),
		mcpServerRefs:  req.GetStringSlice("mcpServerRefs", nil),
		port:           port,
		gitAuthorName:  gitAuthorName,
		gitAuthorEmail: gitAuthorEmail,
		gitCredHelper:  req.GetString("gitCredentialHelper", ""),
		gitHTTPS:       req.GetBool("gitHttpsInsteadOfSsh", false),
		persistentMode: req.GetBool("persistentMode", false),
		noIsolate:      req.GetBool("noIsolate", false),
		noFetch:        req.GetBool("noFetch", false),
		permissionMode: req.GetString("permissionMode", ""),
		model:          req.GetString("model", ""),
		systemPrompt:   req.GetString("systemPrompt", ""),
	}

	if _, ok := args["maxBudgetUsd"]; ok {
		b := req.GetFloat("maxBudgetUsd", 0)
		p.maxBudgetUSD = &b
	}

	return p, nil
}

// mcpCreateInstance creates and starts a new instance from MCP parameters.
// It handles name suffix generation, collision detection, config generation,
// directory setup, and starting the container. Returns the create result.
func mcpCreateInstance(ctx context.Context, params *mcpCreateParams, sc *server.ServerContext) (*createResult, error) {
	name := params.name
	if params.generateSuffix {
		suffixed, err := instance.AppendSuffix(name)
		if err != nil {
			return nil, fmt.Errorf("generating name suffix: %v", err)
		}
		name = suffixed
	}

	if err := config.ValidateInstanceName(name); err != nil {
		return nil, err
	}

	instancePaths := sc.InstancePaths(name)

	// Check for name collision before expensive network calls.
	collision, err := instance.CheckCollision(ctx, instancePaths)
	if err != nil {
		return nil, fmt.Errorf("checking for existing instance: %v", err)
	}

	if err := handleMCPCollision(ctx, name, collision, params.force, params.confirm, instancePaths, sc); err != nil {
		return nil, err
	}

	resolver := sc.SourceResolver()
	if params.sourceFilter != "" {
		resolver, err = resolver.ForSource(params.sourceFilter)
		if err != nil {
			return nil, err
		}
	}

	personality, toolchain, pluginArgs, err := orchestrator.ResolveCreateRefs(ctx, resolver, params.personality, params.toolchain, params.pluginArgs)
	if err != nil {
		return nil, fmt.Errorf("resolving refs: %v", err)
	}

	createOpts := config.CreateOptions{
		Name:                 name,
		Workspace:            params.workspace,
		PersistentMode:       params.persistentMode,
		NoIsolate:            params.noIsolate,
		NoFetch:              params.noFetch,
		Personality:          personality,
		Toolchain:            toolchain,
		Plugins:              pluginArgs,
		Port:                 params.port,
		GitAuthorName:        params.gitAuthorName,
		GitAuthorEmail:       params.gitAuthorEmail,
		GitCredentialHelper:  params.gitCredHelper,
		GitHTTPSInsteadOfSSH: params.gitHTTPS,
		EnvVars:              params.envVars,
		EnvForward:           params.envForward,
		McpServers:           params.mcpServers,
		SecretEnvVars:        params.secretEnvVars,
		SecretFiles:          params.secretFiles,
		McpServerRefs:        params.mcpServerRefs,
		PermissionMode:       params.permissionMode,
		Model:                params.model,
		SystemPrompt:         params.systemPrompt,
		MaxBudgetUSD:         params.maxBudgetUSD,
		Context:              ctx,
		Output:               io.Discard,
		ResolvePersonality: func(ctx context.Context, ref string, w io.Writer) (*config.ResolvedPersonality, error) {
			if err := config.EnsureDir(sc.Paths.PersonalitiesDir); err != nil {
				return nil, fmt.Errorf("creating personalities directory: %w", err)
			}
			client := orchestrator.NewDefaultClient()
			pr, err := orchestrator.ResolvePersonality(ctx, client, ref, sc.Paths.PersonalitiesDir, io.Discard)
			if err != nil {
				return nil, err
			}
			plugins, err := orchestrator.ResolvePluginRefs(ctx, client, pr.Spec.Plugins)
			if err != nil {
				return nil, fmt.Errorf("resolving personality plugins: %w", err)
			}
			image, err := client.ResolveToolchainRef(ctx, pr.Spec.Toolchain.Ref())
			if err != nil {
				return nil, fmt.Errorf("resolving personality image: %w", err)
			}

			return &config.ResolvedPersonality{
				Plugins: plugins,
				Image:   image,
			}, nil
		},
	}

	cfg, err := config.GenerateInstanceConfig(sc.Paths, createOpts)
	if err != nil {
		return nil, fmt.Errorf("generating config: %v", err)
	}

	if err := config.EnsureDir(instancePaths.InstanceDir); err != nil {
		return nil, fmt.Errorf("creating instance directory: %v", err)
	}
	data, err := cfg.Marshal()
	if err != nil {
		return nil, fmt.Errorf("serializing config: %v", err)
	}
	if err := os.WriteFile(instancePaths.ConfigFile, data, 0o644); err != nil {
		return nil, fmt.Errorf("writing instance config: %v", err)
	}

	if err := config.EnsureDir(filepath.Dir(instancePaths.RenderedDir)); err != nil {
		return nil, fmt.Errorf("creating rendered directory parent: %v", err)
	}

	result, err := startExistingInstance(ctx, name, sc)
	if err != nil {
		if cfg.WorktreePath != "" {
			_ = worktree.Remove(cfg.Workspace, cfg.WorktreePath)
		}
		_ = os.RemoveAll(instancePaths.InstanceDir)
		return nil, err
	}
	return result, nil
}
