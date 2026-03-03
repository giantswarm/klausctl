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

var resultOutput string

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
	rootCmd.AddCommand(resultCmd)
}

type resultCLIResult struct {
	Instance     string `json:"instance"`
	Status       string `json:"status"`
	MessageCount int    `json:"message_count"`
	Result       string `json:"result,omitempty"`
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

	toolResult, err := client.Result(ctx, instanceName, baseURL)
	if err != nil {
		return fmt.Errorf("fetching result from %q: %w", instanceName, err)
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
			Result:   extractMCPText(toolResult),
		}
	}

	text := extractMCPText(toolResult)

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
