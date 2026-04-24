package instance

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/klausctl/internal/remotesurface"
	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/agentclient"
	"github.com/giantswarm/klausctl/pkg/mcpclient"
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
		mcp.WithString("mode", mcp.Description(`Operating mode: "agent" (default, autonomous coding, new process per prompt) or "chat" (interactive, persistent process, saved sessions)`)),
		mcp.WithBoolean("noIsolate", mcp.Description("Skip git worktree creation and bind-mount workspace directly (default: false)")),
		mcp.WithBoolean("noFetch", mcp.Description("Skip git fetch origin before cloning the workspace (default: false)")),
		mcp.WithNumber("port", mcp.Description("Override auto-selected host port for the instance MCP endpoint (0 or omitted = auto-select starting from 8080)")),
		mcp.WithString("gitAuthor", mcp.Description("Git author identity as \"Name <email>\"; sets GIT_AUTHOR_NAME/GIT_COMMITTER_NAME and GIT_AUTHOR_EMAIL/GIT_COMMITTER_EMAIL in the container")),
		mcp.WithString("gitCredentialHelper", mcp.Description("Git credential helper (currently only \"gh\" is supported, which configures git to call \"gh auth git-credential\" for github.com)")),
		mcp.WithBoolean("gitHttpsInsteadOfSsh", mcp.Description("Rewrite SSH git URLs (git@github.com:...) to HTTPS via container-local gitconfig (default: false)")),
		mcp.WithBoolean("generateSuffix", mcp.Description("Append a random 4-character suffix to the instance name to avoid collisions (default: true)")),
		mcp.WithBoolean("force", mcp.Description("Allow replacing a running instance; requires confirm: true as well")),
		mcp.WithBoolean("confirm", mcp.Description("Confirm replacement of an existing instance; required when a name collision is detected")),
		mcp.WithBoolean("blocking", mcp.Description("Wait for the agent to complete and return the result (default: false)")),
	)
	addRemoteMCPInputs(&tool)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleRun(ctx, req, sc)
	})
}

// addRemoteMCPInputs mutates an existing mcp.Tool to attach the
// `remote` and `session` inputs declared in internal/remotesurface so
// the MCP tool surface stays in lock-step with the CLI flags.
func addRemoteMCPInputs(tool *mcp.Tool) {
	for _, f := range remotesurface.Flags {
		applyMCPInput(tool, f)
	}
}

func applyMCPInput(tool *mcp.Tool, f remotesurface.Flag) {
	var opt mcp.ToolOption
	switch f.Kind {
	case "bool":
		opt = mcp.WithBoolean(f.MCPKey, mcp.Description(f.Description))
	case "int":
		opt = mcp.WithNumber(f.MCPKey, mcp.Description(f.Description))
	default:
		opt = mcp.WithString(f.MCPKey, mcp.Description(f.Description))
	}
	opt(tool)
}

type runResult struct {
	Instance  string `json:"instance"`
	Status    string `json:"status"`
	Container string `json:"container,omitempty"`
	Image     string `json:"image,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Port      int    `json:"port,omitempty"`
	Result    string `json:"result,omitempty"`
}

func handleRun(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	message, err := req.RequireString("message")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	blocking := req.GetBool("blocking", false)

	if name := req.GetString("name", ""); name != "" && req.GetString("remote", "") != "" {
		return handleRunRemote(ctx, req, sc, name, message, blocking)
	}

	// Create the instance using the shared create logic.
	params, err := parseMCPCreateParams(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	createRes, err := mcpCreateInstance(ctx, params, sc)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	name := createRes.Instance
	instancePaths := sc.InstancePaths(name)

	// cleanupOnError stops the container and removes instance state if a
	// post-start step (MCP readiness, prompt) fails.
	cleanupOnError := func(stepErr error) (*mcp.CallToolResult, error) {
		// Best-effort: stop and remove the container we just started.
		_ = cleanupContainer(context.Background(), name, nil)
		_ = os.RemoveAll(instancePaths.InstanceDir)
		return mcp.NewToolResultError(stepErr.Error()), nil
	}

	agentURL := fmt.Sprintf("http://localhost:%d", createRes.Port)
	httpClient := &http.Client{}

	if err := agentclient.WaitForReady(ctx, httpClient, agentURL); err != nil {
		return cleanupOnError(fmt.Errorf("waiting for instance %q to become ready: %v", name, err))
	}

	// Send the prompt via the chat completions API.
	compCh, err := agentclient.StreamCompletion(ctx, httpClient, agentclient.CompletionRequest{
		URL:    agentURL + "/v1/chat/completions",
		Prompt: message,
	})
	if err != nil {
		return cleanupOnError(fmt.Errorf("sending prompt to %q: %v", name, err))
	}

	if !blocking {
		go func() {
			for range compCh {
			}
		}()
		return server.JSONResult(runResult{
			Instance:  name,
			Status:    "started",
			Container: createRes.Container,
			Image:     createRes.Image,
			Workspace: createRes.Workspace,
			Port:      createRes.Port,
		})
	}

	// Drain the completions stream to let the agent finish.
	for range compCh {
	}

	baseURL := fmt.Sprintf("http://localhost:%d/mcp", createRes.Port)
	resultResp, err := sc.MCPClient.Result(ctx, name, baseURL, false)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("fetching result from %q: %v", name, err)), nil
	}

	return server.JSONResult(runResult{
		Instance:  name,
		Status:    "completed",
		Container: createRes.Container,
		Image:     createRes.Image,
		Workspace: createRes.Workspace,
		Port:      createRes.Port,
		Result:    mcpclient.ExtractText(resultResp),
	})
}

// handleRunRemote services klaus_run when the `remote` input is set: no
// container is created and no local state is touched; the prompt flows
// straight through to the named instance on the remote gateway.
func handleRunRemote(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext, name, message string, blocking bool) (*mcp.CallToolResult, error) {
	call, err := remoteFromReq(ctx, req, sc, name)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	httpClient := &http.Client{}
	compCh, err := call.streamRemotePrompt(ctx, httpClient, message)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("sending prompt to %q via %s: %v", name, call.Target.BaseURL, err)), nil
	}

	if !blocking {
		go func() {
			for range compCh {
			}
		}()
		return server.JSONResult(runResult{
			Instance:  name,
			Status:    "started",
			Workspace: call.Target.BaseURL,
		})
	}

	var builder []byte
	for delta := range compCh {
		if delta.Err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("streaming from %q: %v", name, delta.Err)), nil
		}
		builder = append(builder, delta.Content...)
	}

	return server.JSONResult(runResult{
		Instance:  name,
		Status:    "completed",
		Workspace: call.Target.BaseURL,
		Result:    string(builder),
	})
}
