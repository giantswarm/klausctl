package instance

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

func registerPrompt(s *mcpserver.MCPServer, sc *server.ServerContext) {
	tool := mcp.NewTool("klaus_prompt",
		mcp.WithDescription("Send a prompt to a running klaus instance and optionally wait for the result"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Instance name")),
		mcp.WithString("message", mcp.Required(), mcp.Description("Prompt message to send to the agent")),
		mcp.WithBoolean("blocking", mcp.Description("Wait for the agent to complete and return the result (default: false)")),
	)
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
	Instance  string `json:"instance"`
	Status    string `json:"status"`
	SessionID string `json:"session_id,omitempty"`
	Result    string `json:"result,omitempty"`
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

	baseURL, err := agentBaseURL(ctx, name, sc)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	toolResult, err := sc.MCPClient.Prompt(ctx, name, baseURL, message)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("sending prompt to %q: %v", name, err)), nil
	}

	if !blocking {
		return server.JSONResult(promptResult{
			Instance:  name,
			Status:    "started",
			SessionID: sc.MCPClient.SessionID(name),
			Result:    extractText(toolResult),
		})
	}

	result, err := waitForResult(ctx, name, baseURL, sc)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("waiting for result from %q: %v", name, err)), nil
	}

	return server.JSONResult(promptResult{
		Instance:  name,
		Status:    "completed",
		SessionID: sc.MCPClient.SessionID(name),
		Result:    result,
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
			Result:   extractText(toolResult),
		})
	}

	// When full is requested, pass the raw agent JSON through without
	// re-parsing into the reduced agentResult struct.
	if full {
		text := extractText(toolResult)
		return mcp.NewToolResultText(text), nil
	}

	text := extractText(toolResult)

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
		mcp.WithDescription("Retrieve conversation messages from a running klaus instance"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Instance name")),
		mcp.WithBoolean("follow", mcp.Description("Poll for new messages until the agent completes (default: false)")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleMessages(ctx, req, sc)
	})
}

type messagesResult struct {
	Instance string           `json:"instance"`
	Status   string           `json:"status"`
	Count    int              `json:"count"`
	Messages []agentMessageMC `json:"messages"`
}

type agentMessageMC struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type agentMessagesToolResponse struct {
	Status   string           `json:"status"`
	Messages []agentMessageMC `json:"messages"`
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

	baseURL, err := agentBaseURL(ctx, name, sc)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if !follow {
		return fetchMessages(ctx, name, baseURL, sc)
	}

	return followMessagesUntilDone(ctx, name, baseURL, sc)
}

func fetchMessages(ctx context.Context, name, baseURL string, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	toolResult, err := sc.MCPClient.Messages(ctx, name, baseURL)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("fetching messages from %q: %v", name, err)), nil
	}

	if toolResult.IsError {
		return server.JSONResult(messagesResult{
			Instance: name,
			Status:   "error",
			Messages: []agentMessageMC{},
		})
	}

	text := extractText(toolResult)
	var parsed agentMessagesToolResponse
	if err := json.Unmarshal([]byte(text), &parsed); err == nil && parsed.Status != "" {
		return server.JSONResult(messagesResult{
			Instance: name,
			Status:   parsed.Status,
			Count:    len(parsed.Messages),
			Messages: parsed.Messages,
		})
	}

	return server.JSONResult(messagesResult{
		Instance: name,
		Status:   "unknown",
		Messages: []agentMessageMC{},
	})
}

func followMessagesUntilDone(ctx context.Context, name, baseURL string, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	const maxFollowDuration = 30 * time.Minute
	ctx, cancel := context.WithTimeout(ctx, maxFollowDuration)
	defer cancel()

	poll := 2 * time.Second
	const maxPoll = 10 * time.Second

	var lastResult *mcp.CallToolResult

	for {
		toolResult, err := sc.MCPClient.Messages(ctx, name, baseURL)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("fetching messages from %q: %v", name, err)), nil
		}

		text := extractText(toolResult)
		var parsed agentMessagesToolResponse
		if json.Unmarshal([]byte(text), &parsed) == nil {
			lastResult, _ = server.JSONResult(messagesResult{
				Instance: name,
				Status:   parsed.Status,
				Count:    len(parsed.Messages),
				Messages: parsed.Messages,
			})
			if terminalStatuses[parsed.Status] {
				return lastResult, nil
			}
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

// terminalStatuses are the agent statuses that indicate the task is done.
var terminalStatuses = map[string]bool{
	"completed": true,
	"error":     true,
	"failed":    true,
}

// waitForResult polls the agent's status tool until the task completes or the
// context is cancelled, then retrieves the result.
func waitForResult(ctx context.Context, name, baseURL string, sc *server.ServerContext) (string, error) {
	poll := 2 * time.Second
	const maxPoll = 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(poll):
		}

		statusResult, err := sc.MCPClient.Status(ctx, name, baseURL)
		if err != nil {
			return "", fmt.Errorf("polling status: %w", err)
		}

		if terminalStatuses[parseStatusField(statusResult)] {
			break
		}

		if poll < maxPoll {
			poll = min(poll*2, maxPoll)
		}
	}

	resultResp, err := sc.MCPClient.Result(ctx, name, baseURL, false)
	if err != nil {
		return "", fmt.Errorf("fetching result: %w", err)
	}

	return extractText(resultResp), nil
}

// extractText returns the concatenated text content from an MCP tool result.
func extractText(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}

	var parts []string
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}

	raw, err := json.Marshal(result.Content)
	if err != nil {
		return ""
	}
	return string(raw)
}

// parseStatusField extracts the "status" field from a JSON tool result.
// Returns the status string if found, or empty string otherwise.
func parseStatusField(result *mcp.CallToolResult) string {
	text := extractText(result)
	if text == "" {
		return ""
	}
	var parsed struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err == nil && parsed.Status != "" {
		return parsed.Status
	}
	return text
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

	return parseStatusField(result)
}
