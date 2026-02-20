package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/mcpclient"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

var (
	promptMessage  string
	promptBlocking bool
	promptOutput   string
)

var promptCmd = &cobra.Command{
	Use:   "prompt [name]",
	Short: "Send a prompt to a running klaus instance",
	Long: `Send a prompt message to the agent running inside a klaus instance.

By default the command returns immediately after the prompt is accepted.
Use --blocking to wait for the agent to finish and print the result.

Examples:

  klausctl prompt dev -m "List all Go files in the workspace"
  klausctl prompt dev -m "Fix the failing test" --blocking
  klausctl prompt dev -m "Refactor the handler" -o json`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPrompt,
}

func init() {
	promptCmd.Flags().StringVarP(&promptMessage, "message", "m", "", "prompt message to send to the agent (required)")
	promptCmd.Flags().BoolVar(&promptBlocking, "blocking", false, "wait for the agent to complete and return the result")
	promptCmd.Flags().StringVarP(&promptOutput, "output", "o", "text", "output format: text, json")
	_ = promptCmd.MarkFlagRequired("message")
	rootCmd.AddCommand(promptCmd)
}

type promptCLIResult struct {
	Instance  string `json:"instance"`
	Status    string `json:"status"`
	SessionID string `json:"session_id,omitempty"`
	Result    string `json:"result,omitempty"`
}

func runPrompt(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	out := cmd.OutOrStdout()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := config.MigrateLayout(paths); err != nil {
		return fmt.Errorf("migrating config layout: %w", err)
	}

	instanceName, err := resolveOptionalInstanceName(args, "prompt", cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	paths = paths.ForInstance(instanceName)

	inst, err := instance.Load(paths)
	if err != nil {
		return fmt.Errorf("no klaus instance found for %q; run 'klausctl create %s <workspace>' first", instanceName, instanceName)
	}

	rt, err := runtime.New(inst.Runtime)
	if err != nil {
		return err
	}

	status, err := rt.Status(ctx, inst.ContainerName())
	if err != nil {
		return fmt.Errorf("instance %q: unable to determine status: %w", instanceName, err)
	}
	if status != "running" {
		return fmt.Errorf("instance %q is not running (status: %s); run 'klausctl start %s' first", instanceName, status, instanceName)
	}

	baseURL := fmt.Sprintf("http://localhost:%d/mcp", inst.Port)

	client := mcpclient.New(buildVersion)
	defer client.Close()

	toolResult, err := client.Prompt(ctx, instanceName, baseURL, promptMessage)
	if err != nil {
		return fmt.Errorf("sending prompt to %q: %w", instanceName, err)
	}

	if !promptBlocking {
		result := promptCLIResult{
			Instance:  instanceName,
			Status:    "started",
			SessionID: client.SessionID(instanceName),
			Result:    extractMCPText(toolResult),
		}
		return renderPromptResult(out, result)
	}

	agentResult, err := waitForAgentResult(ctx, instanceName, baseURL, client)
	if err != nil {
		return fmt.Errorf("waiting for result from %q: %w", instanceName, err)
	}

	result := promptCLIResult{
		Instance:  instanceName,
		Status:    "completed",
		SessionID: client.SessionID(instanceName),
		Result:    agentResult,
	}
	return renderPromptResult(out, result)
}

func renderPromptResult(out io.Writer, result promptCLIResult) error {
	if promptOutput == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Fprintf(out, "Instance: %s\n", result.Instance)
	fmt.Fprintf(out, "Status:   %s\n", colorStatus(result.Status))
	if result.SessionID != "" {
		fmt.Fprintf(out, "Session:  %s\n", result.SessionID)
	}
	if result.Result != "" {
		fmt.Fprintf(out, "\n%s\n", result.Result)
	}
	return nil
}

// waitForAgentResult polls the agent's status until the task completes, then
// retrieves the result.
func waitForAgentResult(ctx context.Context, name, baseURL string, client *mcpclient.Client) (string, error) {
	poll := 2 * time.Second
	const maxPoll = 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(poll):
		}

		statusResult, err := client.Status(ctx, name, baseURL)
		if err != nil {
			return "", fmt.Errorf("polling status: %w", err)
		}

		if agentTerminalStatuses[parseAgentStatusField(statusResult)] {
			break
		}

		if poll < maxPoll {
			poll = min(poll*2, maxPoll)
		}
	}

	resultResp, err := client.Result(ctx, name, baseURL)
	if err != nil {
		return "", fmt.Errorf("fetching result: %w", err)
	}

	return extractMCPText(resultResp), nil
}

var agentTerminalStatuses = map[string]bool{
	"completed": true,
	"error":     true,
	"failed":    true,
}

// extractMCPText returns the concatenated text content from an MCP tool result.
// Only TextContent items are extracted. Non-text content types (images, etc.)
// are silently skipped; callers needing the raw payload should inspect
// result.Content directly.
func extractMCPText(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}

	var parts []string
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// parseAgentStatusField extracts the "status" field from a JSON tool result.
func parseAgentStatusField(result *mcp.CallToolResult) string {
	text := extractMCPText(result)
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
