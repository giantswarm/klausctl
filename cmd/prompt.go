package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/agentclient"
	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
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
	Instance string `json:"instance"`
	Status   string `json:"status"`
	Result   string `json:"result,omitempty"`
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

	agentURL := fmt.Sprintf("http://localhost:%d", inst.Port)
	httpClient := &http.Client{}

	if promptBlocking {
		return runPromptBlocking(ctx, out, httpClient, agentURL, instanceName)
	}

	return runPromptNonBlocking(ctx, out, httpClient, agentURL, instanceName)
}

// runPromptBlocking sends the prompt via the chat completions streaming API
// and prints content deltas as they arrive.
func runPromptBlocking(ctx context.Context, out io.Writer, httpClient *http.Client, agentURL, instanceName string) error {
	compCh, err := agentclient.StreamCompletion(ctx, httpClient, agentURL, promptMessage)
	if err != nil {
		return fmt.Errorf("sending prompt to %q: %w", instanceName, err)
	}

	for delta := range compCh {
		if delta.Err != nil {
			return fmt.Errorf("streaming from %q: %w", instanceName, delta.Err)
		}
		fmt.Fprint(out, delta.Content)
	}
	fmt.Fprintln(out)

	return renderPromptResult(out, promptCLIResult{
		Instance: instanceName,
		Status:   "completed",
	})
}

// runPromptNonBlocking sends the prompt via the completions API and returns
// immediately.
func runPromptNonBlocking(ctx context.Context, out io.Writer, httpClient *http.Client, agentURL, instanceName string) error {
	compCh, err := agentclient.StreamCompletion(ctx, httpClient, agentURL, promptMessage)
	if err != nil {
		return fmt.Errorf("sending prompt to %q: %w", instanceName, err)
	}
	go func() {
		for range compCh {
		}
	}()

	result := promptCLIResult{
		Instance: instanceName,
		Status:   "started",
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
	if result.Result != "" {
		fmt.Fprintf(out, "\n%s\n", result.Result)
	}
	return nil
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

