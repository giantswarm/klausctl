package instance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/agentclient"
	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/mcpclient"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

func registerPrompt(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_prompt",
		mcp.WithDescription("Send a prompt to a running klaus instance and optionally wait for the result"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Instance name")),
		mcp.WithString("message", mcp.Required(), mcp.Description("Prompt message to send to the agent")),
		mcp.WithBoolean("blocking", mcp.Description("Wait for the agent to complete and return the result (default: false)")),
	)
	addRemoteMCPInputs(&tool)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handlePrompt(ctx, req, sc)
	})
}

func registerResult(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_result",
		mcp.WithDescription("Retrieve the result from the last prompt sent to a klaus instance"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Instance name")),
		mcp.WithBoolean("full", mcp.Description("Return full agent detail including tool_calls, model_usage, token_usage, cost, etc.")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleResult(ctx, req, sc)
	})
}

type promptResult struct {
	Instance string `json:"instance"`
	Status   string `json:"status"`
	Result   string `json:"result,omitempty"`
}

func handlePrompt(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	message, err := req.RequireString("message")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	blocking := req.GetBool("blocking", false)

	if req.GetString("remote", "") != "" {
		return handlePromptRemote(ctx, req, sc, name, message, blocking)
	}

	baseURL, err := agentBaseURL(ctx, name, sc)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	agentURL := strings.TrimSuffix(baseURL, "/mcp")
	httpClient := &http.Client{}

	compCh, err := agentclient.StreamCompletion(ctx, httpClient, agentclient.CompletionRequest{
		URL:    agentURL + "/v1/chat/completions",
		Prompt: message,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("sending prompt to %q: %v", name, err)), nil
	}

	if !blocking {
		go func() {
			for range compCh {
			}
		}()
		return server.JSONResult(promptResult{
			Instance: name,
			Status:   "started",
		})
	}

	for range compCh {
	}

	resultResp, err := sc.MCPClient.Result(ctx, name, baseURL, false)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("fetching result from %q: %v", name, err)), nil
	}

	return server.JSONResult(promptResult{
		Instance: name,
		Status:   "completed",
		Result:   mcpclient.ExtractText(resultResp),
	})
}

// handlePromptRemote services klaus_prompt when `remote` is set: the
// prompt is forwarded to <remote>/v1/<name>/chat/completions with the
// routing headers and bearer attached.
func handlePromptRemote(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext, name, message string, blocking bool) (*mcp.CallToolResult, error) {
	call, err := remoteFromReq(ctx, req, sc, name)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	compCh, err := call.streamRemotePrompt(ctx, &http.Client{}, message)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("sending prompt to %q via %s: %v", name, call.Target.BaseURL, err)), nil
	}

	if !blocking {
		go func() {
			for range compCh {
			}
		}()
		return server.JSONResult(promptResult{
			Instance: name,
			Status:   "started",
		})
	}

	var builder []byte
	for delta := range compCh {
		if delta.Err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("streaming from %q: %v", name, delta.Err)), nil
		}
		builder = append(builder, delta.Content...)
	}

	return server.JSONResult(promptResult{
		Instance: name,
		Status:   "completed",
		Result:   string(builder),
	})
}

type agentResult struct {
	Instance     string `json:"instance"`
	Status       string `json:"status"`
	MessageCount int    `json:"message_count"`
	Result       string `json:"result,omitempty"`
}

// agentToolResponse represents the JSON payload returned by the agent's
// result MCP tool inside the container.
type agentToolResponse struct {
	Status       string `json:"status"`
	MessageCount int    `json:"message_count"`
	ResultText   string `json:"result_text"`
}

func handleResult(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	full := req.GetBool("full", false)

	baseURL, err := agentBaseURL(ctx, name, sc)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	toolResult, err := sc.MCPClient.Result(ctx, name, baseURL, full)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("fetching result from %q: %v", name, err)), nil
	}

	if toolResult.IsError {
		return server.JSONResult(agentResult{
			Instance: name,
			Status:   "error",
			Result:   mcpclient.ExtractText(toolResult),
		})
	}

	// When full is requested, pass the raw agent JSON through without
	// re-parsing into the reduced agentResult struct.
	if full {
		text := mcpclient.ExtractText(toolResult)
		return mcp.NewToolResultText(text), nil
	}

	text := mcpclient.ExtractText(toolResult)

	var parsed agentToolResponse
	if err := json.Unmarshal([]byte(text), &parsed); err == nil && parsed.Status != "" {
		return server.JSONResult(agentResult{
			Instance:     name,
			Status:       parsed.Status,
			MessageCount: parsed.MessageCount,
			Result:       parsed.ResultText,
		})
	}

	// Fallback: response is not the expected JSON structure.
	return server.JSONResult(agentResult{
		Instance: name,
		Status:   "completed",
		Result:   text,
	})
}

func registerMessages(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_messages",
		mcp.WithDescription("Retrieve conversation messages from a running klaus instance in OpenAI-compatible format"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Instance name")),
		mcp.WithNumber("offset", mcp.Description("Start returning messages from this index (0-based)")),
		mcp.WithString("types", mcp.Description("Comma-separated message types to include (e.g. 'user,assistant,tool')")),
		mcp.WithBoolean("follow", mcp.Description("Poll for new messages until the agent completes (default: false)")),
	)
	addRemoteMCPInputs(&tool)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleMessages(ctx, req, sc)
	})
}

func handleMessages(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := config.ValidateInstanceName(name); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	follow := req.GetBool("follow", false)
	opts := messagesOptsFromReq(req)

	if req.GetString("remote", "") != "" {
		return handleMessagesRemote(ctx, req, sc, name, follow, opts)
	}

	baseURL, err := agentBaseURL(ctx, name, sc)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if !follow {
		return fetchMessages(ctx, name, baseURL, sc, opts)
	}

	return followMessagesUntilDone(ctx, name, baseURL, sc, opts)
}

// handleMessagesRemote routes the MCP messages tool call to a remote
// klaus-gateway, wrapping mcpclient with the routing headers and bearer.
func handleMessagesRemote(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext, name string, follow bool, opts *mcpclient.MessagesOpts) (*mcp.CallToolResult, error) {
	call, err := remoteFromReq(ctx, req, sc, name)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	client := mcpclient.NewWithHeaders("mcp", call.mcpHeaders())
	defer client.Close()

	baseURL := call.Target.MCPURL()
	if !follow {
		toolResult, err := client.Messages(ctx, name, baseURL, opts)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetching messages from %q via %s: %v", name, call.Target.BaseURL, err)), nil
		}
		if toolResult.IsError {
			return toolResult, nil
		}
		return mcp.NewToolResultText(mcpclient.ExtractText(toolResult)), nil
	}

	return followRemoteMessages(ctx, name, baseURL, client, opts)
}

func followRemoteMessages(ctx context.Context, name, baseURL string, client *mcpclient.Client, opts *mcpclient.MessagesOpts) (*mcp.CallToolResult, error) {
	const maxFollowDuration = 30 * time.Minute
	ctx, cancel := context.WithTimeout(ctx, maxFollowDuration)
	defer cancel()

	poll := 2 * time.Second
	const maxPoll = 10 * time.Second

	var lastResult *mcp.CallToolResult

	for {
		toolResult, err := client.Messages(ctx, name, baseURL, opts)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetching messages from %q: %v", name, err)), nil
		}
		lastResult = mcp.NewToolResultText(mcpclient.ExtractText(toolResult))

		statusResult, statusErr := client.Status(ctx, name, baseURL)
		status := ""
		if statusErr == nil {
			status = mcpclient.ParseStatusField(statusResult)
		}
		if mcpclient.IsTerminalStatus(status) {
			return lastResult, nil
		}

		select {
		case <-ctx.Done():
			if lastResult != nil {
				return lastResult, nil
			}
			return mcp.NewToolResultError("timed out waiting for messages"), nil
		case <-time.After(poll):
		}

		if poll < maxPoll {
			poll = min(poll*2, maxPoll)
		}
	}
}

func messagesOptsFromReq(req mcp.CallToolRequest) *mcpclient.MessagesOpts {
	offset := max(int(req.GetFloat("offset", 0)), 0)
	types := req.GetString("types", "")
	if offset == 0 && types == "" {
		return nil
	}
	return &mcpclient.MessagesOpts{Offset: offset, Types: types}
}

func fetchMessages(ctx context.Context, name, baseURL string, sc *server.ServerContext, opts *mcpclient.MessagesOpts) (*mcp.CallToolResult, error) {
	toolResult, err := sc.MCPClient.Messages(ctx, name, baseURL, opts)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("fetching messages from %q: %v", name, err)), nil
	}

	if toolResult.IsError {
		return toolResult, nil
	}

	return mcp.NewToolResultText(mcpclient.ExtractText(toolResult)), nil
}

func followMessagesUntilDone(ctx context.Context, name, baseURL string, sc *server.ServerContext, opts *mcpclient.MessagesOpts) (*mcp.CallToolResult, error) {
	const maxFollowDuration = 30 * time.Minute
	ctx, cancel := context.WithTimeout(ctx, maxFollowDuration)
	defer cancel()

	poll := 2 * time.Second
	const maxPoll = 10 * time.Second

	var lastResult *mcp.CallToolResult

	for {
		toolResult, err := sc.MCPClient.Messages(ctx, name, baseURL, opts)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetching messages from %q: %v", name, err)), nil
		}

		lastResult = mcp.NewToolResultText(mcpclient.ExtractText(toolResult))

		statusResult, statusErr := sc.MCPClient.Status(ctx, name, baseURL)
		status := ""
		if statusErr == nil {
			status = mcpclient.ParseStatusField(statusResult)
		}
		if mcpclient.IsTerminalStatus(status) {
			return lastResult, nil
		}

		select {
		case <-ctx.Done():
			if lastResult != nil {
				return lastResult, nil
			}
			return mcp.NewToolResultError("timed out waiting for messages"), nil
		case <-time.After(poll):
		}

		if poll < maxPoll {
			poll = min(poll*2, maxPoll)
		}
	}
}

// agentBaseURL resolves the MCP endpoint URL for a running instance.
func agentBaseURL(ctx context.Context, name string, sc *server.ServerContext) (string, error) {
	paths := sc.InstancePaths(name)
	inst, err := instance.Load(paths)
	if err != nil {
		return "", fmt.Errorf("instance %q not found; use klaus_create first: %w", name, err)
	}

	rt, err := runtime.New(inst.Runtime)
	if err != nil {
		return "", fmt.Errorf("runtime error for %q: %w", name, err)
	}

	status, err := rt.Status(ctx, inst.ContainerName())
	if err != nil || status != "running" {
		return "", fmt.Errorf("instance %q is not running (status: %s); use klaus_start first", name, status)
	}

	return fmt.Sprintf("http://localhost:%d/mcp", inst.Port), nil
}

// queryAgentStatus probes the agent's internal status through its MCP endpoint.
// Returns the parsed status string or empty if the agent is unreachable.
func queryAgentStatus(ctx context.Context, name string, port int, sc *server.ServerContext) string {
	baseURL := fmt.Sprintf("http://localhost:%d/mcp", port)

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	result, err := sc.MCPClient.Status(ctx, name, baseURL)
	if err != nil {
		return ""
	}

	return mcpclient.ParseStatusField(result)
}
