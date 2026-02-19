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
	Instance string `json:"instance"`
	Status   string `json:"status"`
	Result   string `json:"result,omitempty"`
}

func handleResult(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext) (*mcp.CallToolResult, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	baseURL, err := agentBaseURL(ctx, name, sc)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	toolResult, err := sc.MCPClient.Result(ctx, name, baseURL)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("fetching result from %q: %v", name, err)), nil
	}

	text := extractText(toolResult)

	status := "completed"
	if toolResult.IsError {
		status = "error"
	}

	return server.JSONResult(agentResult{
		Instance: name,
		Status:   status,
		Result:   text,
	})
}

// agentBaseURL resolves the MCP endpoint URL for a running instance.
func agentBaseURL(ctx context.Context, name string, sc *server.ServerContext) (string, error) {
	paths := sc.InstancePaths(name)
	inst, err := instance.Load(paths)
	if err != nil {
		return "", fmt.Errorf("instance %q not found; use klaus_create first", name)
	}

	rt, err := runtime.New(inst.Runtime)
	if err != nil {
		return "", fmt.Errorf("runtime error for %q: %v", name, err)
	}

	status, err := rt.Status(ctx, inst.ContainerName())
	if err != nil || status != "running" {
		return "", fmt.Errorf("instance %q is not running (status: %s); use klaus_start first", name, status)
	}

	return fmt.Sprintf("http://localhost:%d/mcp", inst.Port), nil
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

		text := extractText(statusResult)
		if strings.Contains(text, "completed") || strings.Contains(text, "error") {
			break
		}

		if poll < maxPoll {
			poll = min(poll*2, maxPoll)
		}
	}

	resultResp, err := sc.MCPClient.Result(ctx, name, baseURL)
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

// queryAgentStatus probes the agent's internal status through its MCP endpoint.
// Returns the agent status string or empty if the agent is unreachable.
func queryAgentStatus(ctx context.Context, name string, port int, sc *server.ServerContext) string {
	baseURL := fmt.Sprintf("http://localhost:%d/mcp", port)

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	result, err := sc.MCPClient.Status(ctx, name, baseURL)
	if err != nil {
		return ""
	}

	return extractText(result)
}
