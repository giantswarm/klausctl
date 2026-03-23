package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/mcpclient"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

var (
	resultOutput string
	resultFull   bool
)

var resultCmd = &cobra.Command{
	Use:   "result [name]",
	Short: "Retrieve the result from a klaus instance",
	Long: `Retrieve the result from the last prompt sent to a klaus instance.

Examples:

  klausctl result dev
  klausctl result dev -o json`,
	Args: cobra.MaximumNArgs(1),
	RunE: runResult,
}

func init() {
	resultCmd.Flags().StringVarP(&resultOutput, "output", "o", "text", "output format: text, json")
	resultCmd.Flags().BoolVar(&resultFull, "full", false, "include full agent detail in JSON output (tool_calls, model_usage, token_usage, cost, etc.)")
	rootCmd.AddCommand(resultCmd)
}

type resultCLIResult struct {
	Instance     string `json:"instance"`
	Status       string `json:"status"`
	MessageCount int    `json:"message_count"`
	Result       string `json:"result,omitempty"`
}

// fullResultCLIResult includes all fields from the agent's full result
// response. Used when --full is passed with -o json.
type fullResultCLIResult struct {
	Instance      string          `json:"instance"`
	Status        string          `json:"status"`
	MessageCount  int             `json:"message_count"`
	ResultText    string          `json:"result_text,omitempty"`
	SessionID     string          `json:"session_id,omitempty"`
	TotalCostUSD  *float64        `json:"total_cost_usd,omitempty"`
	ToolCalls     map[string]int  `json:"tool_calls,omitempty"`
	ModelUsage    map[string]int  `json:"model_usage,omitempty"`
	TokenUsage    json.RawMessage `json:"token_usage,omitempty"`
	SubagentCalls json.RawMessage `json:"subagent_calls,omitempty"`
	PRURLs        []string        `json:"pr_urls,omitempty"`
	ErrorCount    int             `json:"error_count,omitempty"`
	ErrorMessage  string          `json:"error,omitempty"`
}

// agentResultResponse represents the JSON payload returned by the agent's
// result MCP tool. Fields are extracted to populate resultCLIResult.
type agentResultResponse struct {
	Status       string `json:"status"`
	MessageCount int    `json:"message_count"`
	ResultText   string `json:"result_text"`
}

func runResult(cmd *cobra.Command, args []string) error {
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

	instanceName, err := resolveOptionalInstanceName(args, "result", cmd.ErrOrStderr())
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

	toolResult, err := client.Result(ctx, instanceName, baseURL, resultFull)
	if err != nil {
		return fmt.Errorf("fetching result from %q: %w", instanceName, err)
	}

	// In full JSON mode, pass through the extended agent response.
	if resultFull && resultOutput == "json" {
		return renderFullResultOutput(out, instanceName, toolResult)
	}

	result := parseResultResponse(instanceName, toolResult)

	return renderResultOutput(out, result)
}

// parseResultResponse extracts status, message_count and result_text from the
// agent's MCP result tool response. The agent returns a JSON payload inside
// the MCP TextContent; this function parses that payload so the CLI can display
// accurate status and progress information instead of raw JSON.
func parseResultResponse(instanceName string, toolResult *mcp.CallToolResult) resultCLIResult {
	if toolResult != nil && toolResult.IsError {
		return resultCLIResult{
			Instance: instanceName,
			Status:   "error",
			Result:   mcpclient.ExtractText(toolResult),
		}
	}

	text := mcpclient.ExtractText(toolResult)

	var parsed agentResultResponse
	if err := json.Unmarshal([]byte(text), &parsed); err == nil && parsed.Status != "" {
		return resultCLIResult{
			Instance:     instanceName,
			Status:       parsed.Status,
			MessageCount: parsed.MessageCount,
			Result:       parsed.ResultText,
		}
	}

	// Fallback: response is not the expected JSON structure.
	return resultCLIResult{
		Instance: instanceName,
		Status:   "completed",
		Result:   text,
	}
}

func renderResultOutput(out io.Writer, result resultCLIResult) error {
	if resultOutput == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Fprintf(out, "Instance: %s\n", result.Instance)
	fmt.Fprintf(out, "Status:   %s\n", colorStatus(result.Status))
	fmt.Fprintf(out, "Messages: %d\n", result.MessageCount)
	if result.Status == "busy" {
		fmt.Fprintf(out, "\nAgent is still processing the prompt.\nRun 'klausctl result %s' again to check for updates.\n", result.Instance)
	} else if result.Result != "" {
		fmt.Fprintf(out, "\n%s\n", result.Result)
	}
	return nil
}

// renderFullResultOutput parses the agent's full result response and outputs
// it as a fullResultCLIResult JSON object.
func renderFullResultOutput(out io.Writer, instanceName string, toolResult *mcp.CallToolResult) error {
	text := mcpclient.ExtractText(toolResult)

	result := fullResultCLIResult{Instance: instanceName}

	// Parse the raw JSON from the agent into a generic map, then selectively
	// decode fields into our typed struct.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &raw); err == nil {
		decodeJSONField(raw, "status", &result.Status)
		decodeJSONField(raw, "result_text", &result.ResultText)
		decodeJSONField(raw, "message_count", &result.MessageCount)
		decodeJSONField(raw, "session_id", &result.SessionID)
		decodeJSONField(raw, "total_cost_usd", &result.TotalCostUSD)
		decodeJSONField(raw, "tool_calls", &result.ToolCalls)
		decodeJSONField(raw, "model_usage", &result.ModelUsage)
		decodeJSONField(raw, "pr_urls", &result.PRURLs)
		decodeJSONField(raw, "error_count", &result.ErrorCount)
		decodeJSONField(raw, "error", &result.ErrorMessage)
		if v, ok := raw["token_usage"]; ok {
			result.TokenUsage = v
		}
		if v, ok := raw["subagent_calls"]; ok {
			result.SubagentCalls = v
		}
	} else {
		// Not valid JSON — treat as plain text result.
		result.Status = "completed"
		result.ResultText = text
	}

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

// decodeJSONField unmarshals a single field from a raw JSON map into dst.
func decodeJSONField(raw map[string]json.RawMessage, key string, dst any) {
	if v, ok := raw[key]; ok {
		_ = json.Unmarshal(v, dst)
	}
}
