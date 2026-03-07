package instance

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/orchestrator"
)

func registerRun(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_run",
		mcp.WithDescription("Create a new klaus instance, wait for it to become ready, and send a prompt — all in one operation"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Instance name")),
		mcp.WithString("message", mcp.Required(), mcp.Description("Prompt message to send to the agent after the instance is ready")),
		mcp.WithString("workspace", mcp.Description("Workspace directory (default: current working directory)")),
		mcp.WithString("personality", mcp.Description("Personality short name or OCI reference")),
		mcp.WithString("toolchain", mcp.Description("Toolchain short name or OCI reference")),
		mcp.WithArray("plugin", mcp.Description("Additional plugin short names or OCI references")),
		mcp.WithString("source", mcp.Description("Resolve artifact short names against a specific source")),
		mcp.WithObject("envVars", mcp.Description("Environment variable key-value pairs to set in the container (merged with any existing envVars from the resolved config)")),
		mcp.WithArray("envForward", mcp.Description("Host environment variable names to forward to the container (merged with any existing envForward entries; duplicates are removed)")),
		mcp.WithObject("mcpServers", mcp.Description("MCP server configurations rendered to .mcp.json (merged with any existing mcpServers from the resolved config)")),
		mcp.WithObject("secretEnvVars", mcp.Description("Map of container env var name -> secret name; secrets are resolved from the global secrets store at start time")),
		mcp.WithObject("secretFiles", mcp.Description("Map of container file path -> secret name; secrets are resolved, written to disk, and mounted read-only at the specified path")),
		mcp.WithArray("mcpServerRefs", mcp.Description("Managed MCP server names to include; resolved from the global mcpservers.yaml with optional Bearer token")),
		mcp.WithNumber("maxBudgetUsd", mcp.Description("Maximum dollar budget for the Claude agent per invocation; 0 = no limit (overrides personality default if set)")),
		mcp.WithString("permissionMode", mcp.Description("Claude permission mode (overrides personality default): default, acceptEdits, bypassPermissions, dontAsk, plan, delegate")),
		mcp.WithString("model", mcp.Description("Claude model (overrides personality default, e.g. sonnet, opus, claude-sonnet-4-20250514)")),
		mcp.WithString("systemPrompt", mcp.Description("System prompt for the Claude agent (overrides personality default)")),
		mcp.WithBoolean("noIsolate", mcp.Description("Skip git worktree creation and bind-mount workspace directly (default: false)")),
		mcp.WithNumber("port", mcp.Description("Override auto-selected host port for the instance MCP endpoint (0 or omitted = auto-select starting from 8080)")),
		mcp.WithString("gitAuthor", mcp.Description("Git author identity as \"Name <email>\"; sets GIT_AUTHOR_NAME/GIT_COMMITTER_NAME and GIT_AUTHOR_EMAIL/GIT_COMMITTER_EMAIL in the container")),
		mcp.WithString("gitCredentialHelper", mcp.Description("Git credential helper (currently only \"gh\" is supported, which configures git to call \"gh auth git-credential\" for github.com)")),
		mcp.WithBoolean("gitHttpsInsteadOfSsh", mcp.Description("Rewrite SSH git URLs (git@github.com:...) to HTTPS via container-local gitconfig (default: false)")),
		mcp.WithBoolean("blocking", mcp.Description("Wait for the agent to complete and return the result (default: false)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleRun(ctx, req, sc)
	})
}

type runResult struct {
	Instance  string `json:"instance"`
	Status    string `json:"status"`
	Container string `json:"container,omitempty"`
	Image     string `json:"image,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Port      int    `json:"port,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Result    string `json:"result,omitempty"`
}

func handleRun(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := config.ValidateInstanceName(name); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	message, err := req.RequireString("message")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	blocking := req.GetBool("blocking", false)

	// --- Create the instance (mirrors handleCreate logic) ---

	workspace := req.GetString("workspace", "")
	if workspace == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("determining current directory: %v", err)), nil
		}
		workspace = cwd
	}

	personality := req.GetString("personality", "")
	toolchain := req.GetString("toolchain", "")
	pluginArgs := req.GetStringSlice("plugin", nil)

	resolver := sc.SourceResolver()
	sourceFilter := req.GetString("source", "")
	if sourceFilter != "" {
		resolver, err = resolver.ForSource(sourceFilter)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
	}

	personality, toolchain, pluginArgs, err = orchestrator.ResolveCreateRefs(ctx, resolver, personality, toolchain, pluginArgs)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("resolving refs: %v", err)), nil
	}

	args := req.GetArguments()
	envVars, err := extractStringMap(args, "envVars")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	mcpServers, err := extractObjectMap(args, "mcpServers")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	secretEnvVars, err := extractStringMap(args, "secretEnvVars")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	secretFiles, err := extractStringMap(args, "secretFiles")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	instancePaths := sc.InstancePaths(name)
	if _, err := os.Stat(instancePaths.InstanceDir); err == nil {
		return mcp.NewToolResultError(fmt.Sprintf("instance %q already exists", name)), nil
	}

	port := int(req.GetFloat("port", 0))
	if port < 0 || port > 65535 {
		return mcp.NewToolResultError(fmt.Sprintf("port must be between 1 and 65535, got %d", port)), nil
	}

	gitAuthorName, gitAuthorEmail, err := parseGitAuthor(req.GetString("gitAuthor", ""))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	createOpts := config.CreateOptions{
		Name:                 name,
		Workspace:            workspace,
		NoIsolate:            req.GetBool("noIsolate", false),
		Personality:          personality,
		Toolchain:            toolchain,
		Plugins:              pluginArgs,
		Port:                 port,
		GitAuthorName:        gitAuthorName,
		GitAuthorEmail:       gitAuthorEmail,
		GitCredentialHelper:  req.GetString("gitCredentialHelper", ""),
		GitHTTPSInsteadOfSSH: req.GetBool("gitHttpsInsteadOfSsh", false),
		EnvVars:              envVars,
		EnvForward:           req.GetStringSlice("envForward", nil),
		McpServers:           mcpServers,
		SecretEnvVars:        secretEnvVars,
		SecretFiles:          secretFiles,
		McpServerRefs:        req.GetStringSlice("mcpServerRefs", nil),
		PermissionMode:       req.GetString("permissionMode", ""),
		Model:                req.GetString("model", ""),
		SystemPrompt:         req.GetString("systemPrompt", ""),
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
	if _, ok := args["maxBudgetUsd"]; ok {
		b := req.GetFloat("maxBudgetUsd", 0)
		createOpts.MaxBudgetUSD = &b
	}

	cfg, err := config.GenerateInstanceConfig(sc.Paths, createOpts)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("generating config: %v", err)), nil
	}

	if err := config.EnsureDir(instancePaths.InstanceDir); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("creating instance directory: %v", err)), nil
	}
	data, err := cfg.Marshal()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("serializing config: %v", err)), nil
	}
	if err := os.WriteFile(instancePaths.ConfigFile, data, 0o644); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("writing instance config: %v", err)), nil
	}

	if err := config.EnsureDir(filepath.Dir(instancePaths.RenderedDir)); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("creating rendered directory parent: %v", err)), nil
	}

	createRes, err := startExistingInstance(ctx, name, sc)
	if err != nil {
		_ = os.RemoveAll(instancePaths.InstanceDir)
		return mcp.NewToolResultError(err.Error()), nil
	}

	// cleanupOnError stops the container and removes instance state if a
	// post-start step (MCP readiness, prompt) fails.
	cleanupOnError := func(stepErr error) (*mcp.CallToolResult, error) {
		// Best-effort: stop and remove the container we just started.
		cleanupContainer(context.Background(), name, nil)
		_ = os.RemoveAll(instancePaths.InstanceDir)
		return mcp.NewToolResultError(stepErr.Error()), nil
	}

	// --- Wait for MCP readiness, then send the prompt ---

	baseURL := fmt.Sprintf("http://localhost:%d/mcp", createRes.Port)

	if err := waitForMCPReadyMCP(ctx, name, baseURL, sc); err != nil {
		return cleanupOnError(fmt.Errorf("waiting for instance %q to become ready: %v", name, err))
	}

	toolResult, err := sc.MCPClient.Prompt(ctx, name, baseURL, message)
	if err != nil {
		return cleanupOnError(fmt.Errorf("sending prompt to %q: %v", name, err))
	}

	if !blocking {
		return server.JSONResult(runResult{
			Instance:  name,
			Status:    "started",
			Container: createRes.Container,
			Image:     createRes.Image,
			Workspace: createRes.Workspace,
			Port:      createRes.Port,
			SessionID: sc.MCPClient.SessionID(name),
			Result:    extractText(toolResult),
		})
	}

	agentRes, err := waitForResult(ctx, name, baseURL, sc)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("waiting for result from %q: %v", name, err)), nil
	}

	return server.JSONResult(runResult{
		Instance:  name,
		Status:    "completed",
		Container: createRes.Container,
		Image:     createRes.Image,
		Workspace: createRes.Workspace,
		Port:      createRes.Port,
		SessionID: sc.MCPClient.SessionID(name),
		Result:    agentRes,
	})
}

// waitForMCPReadyMCP polls the agent's MCP endpoint until it responds or
// the context is cancelled / times out.
func waitForMCPReadyMCP(ctx context.Context, name, baseURL string, sc *server.ServerContext) error {
	poll := 2 * time.Second
	const maxPoll = 10 * time.Second
	const maxWait = 2 * time.Minute

	deadline := time.Now().Add(maxWait)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for MCP endpoint", maxWait)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}

		_, err := sc.MCPClient.Status(ctx, name, baseURL)
		if err == nil {
			return nil
		}

		if poll < maxPoll {
			poll = min(poll*2, maxPoll)
		}
	}
}
