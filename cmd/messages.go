package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/mcpclient"
	"github.com/giantswarm/klausctl/pkg/rawmsg"
	"github.com/giantswarm/klausctl/pkg/runtime"
)

var (
	messagesFollow bool
	messagesOutput string
)

var messagesCmd = &cobra.Command{
	Use:   "messages [name]",
	Short: "Display agent conversation messages",
	Long: `Display all messages exchanged with the agent in a running instance.

Each message shows the role (user, assistant, system, tool) and content.

Examples:

  klausctl messages dev
  klausctl messages dev -f
  klausctl messages dev -o json`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMessages,
}

func init() {
	messagesCmd.Flags().BoolVarP(&messagesFollow, "follow", "f", false, "follow new messages in real time")
	messagesCmd.Flags().StringVarP(&messagesOutput, "output", "o", "text", "output format: text, json")
	rootCmd.AddCommand(messagesCmd)
}

type agentMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type agentMessagesResponse struct {
	Status   string         `json:"status"`
	Messages []agentMessage `json:"messages"`
}

type messagesCLIResult struct {
	Instance string         `json:"instance"`
	Status   string         `json:"status"`
	Count    int            `json:"count"`
	Messages []agentMessage `json:"messages"`
}

func runMessages(cmd *cobra.Command, args []string) error {
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

	instanceName, err := resolveOptionalInstanceName(args, "messages", cmd.ErrOrStderr())
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

	if !messagesFollow {
		return fetchAndRenderMessages(ctx, out, client, instanceName, baseURL)
	}

	return followMessages(ctx, out, client, instanceName, baseURL)
}

func fetchAndRenderMessages(ctx context.Context, out io.Writer, client *mcpclient.Client, instanceName, baseURL string) error {
	toolResult, err := client.Messages(ctx, instanceName, baseURL)
	if err != nil {
		return fmt.Errorf("fetching messages from %q: %w", instanceName, err)
	}

	return renderMessages(out, instanceName, toolResult)
}

func followMessages(ctx context.Context, out io.Writer, client *mcpclient.Client, instanceName, baseURL string) error {
	var seen int
	poll := 2 * time.Second
	const maxPoll = 10 * time.Second

	for {
		toolResult, err := client.Messages(ctx, instanceName, baseURL)
		if err != nil {
			return fmt.Errorf("fetching messages from %q: %w", instanceName, err)
		}

		parsed := parseMessagesResponse(toolResult)
		if len(parsed.Messages) > seen {
			for _, msg := range parsed.Messages[seen:] {
				renderSingleMessage(out, msg)
			}
			seen = len(parsed.Messages)
			poll = 2 * time.Second
		}

		if agentTerminalStatuses[parsed.Status] {
			return nil
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(poll):
		}

		if poll < maxPoll {
			poll = min(poll*2, maxPoll)
		}
	}
}

var agentTerminalStatuses = map[string]bool{
	"completed": true,
	"error":     true,
	"failed":    true,
}

func renderMessages(out io.Writer, instanceName string, toolResult *mcp.CallToolResult) error {
	parsed := parseMessagesResponse(toolResult)

	result := messagesCLIResult{
		Instance: instanceName,
		Status:   parsed.Status,
		Count:    len(parsed.Messages),
		Messages: parsed.Messages,
	}

	if messagesOutput == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Fprintf(out, "Instance: %s\n", result.Instance)
	fmt.Fprintf(out, "Status:   %s\n", colorStatus(result.Status))
	fmt.Fprintf(out, "Messages: %d\n\n", result.Count)

	for _, msg := range result.Messages {
		renderSingleMessage(out, msg)
	}

	return nil
}

func renderSingleMessage(out io.Writer, msg agentMessage) {
	fmt.Fprintf(out, "[%s]\n%s\n\n", msg.Role, msg.Content)
}

func parseMessagesResponse(toolResult *mcp.CallToolResult) agentMessagesResponse {
	if toolResult != nil && toolResult.IsError {
		return agentMessagesResponse{Status: "error"}
	}

	text := extractMCPText(toolResult)

	if status, _, msgs, ok := rawmsg.Parse(text); ok {
		converted := make([]agentMessage, len(msgs))
		for i, m := range msgs {
			converted[i] = agentMessage{Role: m.Role, Content: m.Content}
		}
		return agentMessagesResponse{Status: status, Messages: converted}
	}

	var parsed agentMessagesResponse
	if err := json.Unmarshal([]byte(text), &parsed); err == nil && parsed.Status != "" {
		return parsed
	}

	return agentMessagesResponse{Status: "unknown"}
}
