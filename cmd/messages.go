package cmd

import (
	"context"
	"encoding/json"
	"errors"
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

// roleSystem is the message role emitted for system messages.
const roleSystem = "system"

// msgSubtypeInit marks the session-init system message.
const msgSubtypeInit = "init"

var (
	messagesFollow   bool
	messagesOutput   string
	messagesTail     int
	messagesLimit    int
	messagesMaxChars int

	messagesRemote  string
	messagesSession string
)

var messagesCmd = &cobra.Command{
	Use:   "messages [name]",
	Short: "Display agent conversation messages",
	Long: `Display all messages exchanged with the agent in a running instance.

Each message shows the role (user, assistant, system, tool) and content.

Use --tail N to show only the newest N messages, --limit N for the first N,
and --max-chars N to cut long message bodies down to N characters. The header
always reports the full message count, so a small --tail is a cheap way to
check on a long run. With --follow, --tail and --limit select the initial
batch and new messages stream afterwards.

Examples:

  klausctl messages dev
  klausctl messages dev -f
  klausctl messages dev -o json
  klausctl messages dev --tail 3 --max-chars 500
  klausctl messages dev --limit 20`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMessages,
}

func init() {
	messagesCmd.Flags().BoolVarP(&messagesFollow, "follow", "f", false, "follow new messages in real time")
	messagesCmd.Flags().StringVarP(&messagesOutput, "output", "o", outputText, "output format: text, json")
	messagesCmd.Flags().IntVar(&messagesTail, "tail", 0, "show only the last N messages")
	messagesCmd.Flags().IntVar(&messagesLimit, "limit", 0, "show at most N messages, counted from the start")
	messagesCmd.Flags().IntVar(&messagesMaxChars, "max-chars", 0, "truncate each message body to N characters")

	messagesCmd.Flags().StringVar(&messagesRemote, remoteFlagName("remote"), "", remoteFlagDesc("remote"))
	messagesCmd.Flags().StringVar(&messagesSession, remoteFlagName("session"), "", remoteFlagDesc("session"))

	rootCmd.AddCommand(messagesCmd)
}

// agentMessage is the display-ready role/content pair used by the CLI.
type agentMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// messagesEnvelope is the envelope returned by the agent's messages tool.
type messagesEnvelope struct {
	Messages []json.RawMessage       `json:"messages"`
	Metadata *openAIMessagesMetadata `json:"metadata,omitempty"`
	Total    int                     `json:"total"`
}

type openAIMessagesMetadata struct {
	SessionID  string  `json:"session_id,omitempty"`
	Model      string  `json:"model,omitempty"`
	CostUSD    float64 `json:"cost_usd,omitempty"`
	DurationMS int     `json:"duration_ms,omitempty"`
	NumTurns   int     `json:"num_turns,omitempty"`
}

type messagesCLIResult struct {
	Instance string                  `json:"instance"`
	Count    int                     `json:"count"`
	Messages []agentMessage          `json:"messages"`
	Metadata *openAIMessagesMetadata `json:"metadata,omitempty"`
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

	opts, err := messagesOptsFromFlags()
	if err != nil {
		return err
	}

	if messagesRemote != "" {
		return runMessagesRemote(ctx, out, paths, instanceName, opts)
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
	if status != statusRunning {
		return fmt.Errorf("instance %q is not running (status: %s); run 'klausctl start %s' first", instanceName, status, instanceName)
	}

	baseURL := fmt.Sprintf("http://localhost:%d/mcp", inst.Port)

	client := mcpclient.New(buildVersion)
	defer client.Close()

	if !messagesFollow {
		return fetchAndRenderMessages(ctx, out, client, instanceName, baseURL, opts)
	}

	return followMessages(ctx, out, client, instanceName, baseURL, opts)
}

// runMessagesRemote fetches conversation messages from a remote gateway,
// bypassing local runtime state entirely.
func runMessagesRemote(ctx context.Context, out io.Writer, paths *config.Paths, instanceName string, opts *mcpclient.MessagesOpts) error {
	target, _, _, err := resolveRemoteTarget(ctx, messagesRemote, instanceName, messagesSession, paths)
	if err != nil {
		return err
	}

	headers := target.Headers()
	if target.BearerToken != "" {
		headers["Authorization"] = "Bearer " + target.BearerToken
	}

	client := mcpclient.NewWithHeaders(buildVersion, headers)
	defer client.Close()

	baseURL := target.MCPURL()
	if !messagesFollow {
		return fetchAndRenderMessages(ctx, out, client, instanceName, baseURL, opts)
	}
	return followMessages(ctx, out, client, instanceName, baseURL, opts)
}

// messagesOptsFromFlags turns --tail, --limit and --max-chars into
// MessagesOpts. It returns nil when none is set so the agent's response passes
// through unchanged.
func messagesOptsFromFlags() (*mcpclient.MessagesOpts, error) {
	for _, f := range []struct {
		name  string
		value int
	}{{"tail", messagesTail}, {"limit", messagesLimit}, {"max-chars", messagesMaxChars}} {
		if f.value < 0 {
			return nil, fmt.Errorf("--%s must not be negative", f.name)
		}
	}
	if messagesTail > 0 && messagesLimit > 0 {
		return nil, errors.New("--tail and --limit are mutually exclusive")
	}

	opts := mcpclient.MessagesOpts{Limit: messagesLimit, MaxChars: messagesMaxChars}
	if messagesTail > 0 {
		opts.Tail = true
		opts.Limit = messagesTail
	}
	if opts == (mcpclient.MessagesOpts{}) {
		return nil, nil
	}
	return &opts, nil
}

// sliceNote describes the --tail/--limit selection for the text header.
func sliceNote(opts *mcpclient.MessagesOpts) string {
	switch {
	case opts == nil || opts.Limit <= 0:
		return ""
	case opts.Tail:
		return fmt.Sprintf("showing last %d", opts.Limit)
	default:
		return fmt.Sprintf("showing first %d", opts.Limit)
	}
}

func fetchAndRenderMessages(ctx context.Context, out io.Writer, client *mcpclient.Client, instanceName, baseURL string, opts *mcpclient.MessagesOpts) error {
	toolResult, err := client.Messages(ctx, instanceName, baseURL, opts)
	if err != nil {
		return fmt.Errorf("fetching messages from %q: %w", instanceName, err)
	}

	return renderMessages(out, instanceName, toolResult, opts)
}

func followMessages(ctx context.Context, out io.Writer, client *mcpclient.Client, instanceName, baseURL string, opts *mcpclient.MessagesOpts) error {
	// Every poll fetches the full transcript and only --max-chars applies to
	// it; --tail/--limit select the initial batch from the first fetch.
	var pollOpts *mcpclient.MessagesOpts
	if opts != nil && opts.MaxChars > 0 {
		pollOpts = &mcpclient.MessagesOpts{MaxChars: opts.MaxChars}
	}

	var seen int
	first := true
	poll := 2 * time.Second
	const maxPoll = 10 * time.Second

	for {
		toolResult, err := client.Messages(ctx, instanceName, baseURL, nil)
		if err != nil {
			return fmt.Errorf("fetching messages from %q: %w", instanceName, err)
		}

		parsed := parseMessagesResponse(mcpclient.ShapeMessages(toolResult, pollOpts))
		if len(parsed.Messages) > seen {
			fresh := parsed.Messages[seen:]
			if first {
				fresh = parseMessagesResponse(mcpclient.ShapeMessages(toolResult, opts)).Messages
			}
			for _, msg := range fresh {
				renderSingleMessage(out, msg)
			}
			seen = len(parsed.Messages)
			poll = 2 * time.Second
		} else if isAgentDone(ctx, client, instanceName, baseURL) {
			return nil
		}
		first = false

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

func isAgentDone(ctx context.Context, client *mcpclient.Client, instanceName, baseURL string) bool {
	statusResult, err := client.Status(ctx, instanceName, baseURL)
	if err != nil {
		return false
	}
	return mcpclient.IsTerminalStatus(mcpclient.ParseStatusField(statusResult))
}

func renderMessages(out io.Writer, instanceName string, toolResult *mcp.CallToolResult, opts *mcpclient.MessagesOpts) error {
	parsed := parseMessagesResponse(toolResult)

	count := parsed.Total
	if count == 0 {
		count = len(parsed.Messages)
	}

	result := messagesCLIResult{
		Instance: instanceName,
		Count:    count,
		Messages: parsed.Messages,
		Metadata: parsed.Metadata,
	}

	if messagesOutput == outputJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	_, _ = fmt.Fprintf(out, "Instance: %s\n", result.Instance)
	_, _ = fmt.Fprintf(out, "Messages: %d", result.Count)
	if note := sliceNote(opts); note != "" {
		_, _ = fmt.Fprintf(out, " (%s)", note)
	}
	_, _ = fmt.Fprint(out, "\n\n")

	for _, msg := range result.Messages {
		renderSingleMessage(out, msg)
	}

	return nil
}

func renderSingleMessage(out io.Writer, msg agentMessage) {
	_, _ = fmt.Fprintf(out, "[%s]\n%s\n\n", msg.Role, msg.Content)
}

// parsedMessages holds display-ready messages converted from the agent envelope.
type parsedMessages struct {
	Messages []agentMessage
	Metadata *openAIMessagesMetadata
	Total    int
}

func parseMessagesResponse(toolResult *mcp.CallToolResult) parsedMessages {
	if toolResult != nil && toolResult.IsError {
		return parsedMessages{}
	}

	text := mcpclient.ExtractText(toolResult)
	if text == "" {
		return parsedMessages{}
	}

	var envelope messagesEnvelope
	if err := json.Unmarshal([]byte(text), &envelope); err != nil || envelope.Messages == nil {
		return parsedMessages{}
	}

	msgs := make([]agentMessage, 0, len(envelope.Messages))
	for _, raw := range envelope.Messages {
		if msg, ok := convertRawMessage(raw); ok {
			msgs = append(msgs, msg)
		}
	}

	return parsedMessages{
		Messages: msgs,
		Metadata: envelope.Metadata,
		Total:    envelope.Total,
	}
}

// rawMessageItem represents a single message from the agent's messages array.
// The agent wraps each OpenAI-compatible message inside a {"type", "message"} envelope.
type rawMessageItem struct {
	Type    string          `json:"type"`
	Subtype string          `json:"subtype,omitempty"`
	Message json.RawMessage `json:"message,omitempty"`
	Result  string          `json:"result,omitempty"`
}

// openAIMessage is the inner OpenAI-compatible message object.
type openAIMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// openAIContentBlock is a single block inside an OpenAI content array.
type openAIContentBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	Name      string `json:"name,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
}

func convertRawMessage(data json.RawMessage) (agentMessage, bool) {
	// Try flat {role, content} first (future fully-OpenAI-compatible format).
	var flat agentMessage
	if err := json.Unmarshal(data, &flat); err == nil && flat.Role != "" && flat.Content != "" {
		return flat, true
	}

	var item rawMessageItem
	if err := json.Unmarshal(data, &item); err != nil {
		return agentMessage{}, false
	}

	switch item.Type {
	case roleSystem:
		if item.Subtype == msgSubtypeInit || item.Subtype == "hook_started" || item.Subtype == "hook_response" {
			return agentMessage{}, false
		}
		return agentMessage{Role: roleSystem, Content: extractInnerText(item.Message)}, true

	case "assistant":
		text := extractInnerText(item.Message)
		if text == "" {
			return agentMessage{}, false
		}
		return agentMessage{Role: "assistant", Content: text}, true

	case "user":
		text := extractInnerText(item.Message)
		if text == "" {
			return agentMessage{}, false
		}
		return agentMessage{Role: "user", Content: text}, true

	case "result":
		if item.Result != "" {
			return agentMessage{Role: roleSystem, Content: item.Result}, true
		}
		return agentMessage{}, false

	default:
		return agentMessage{}, false
	}
}

// extractInnerText extracts displayable text from the inner OpenAI-compatible
// message object. It handles both string content and array-of-content-blocks.
func extractInnerText(data json.RawMessage) string {
	if len(data) == 0 {
		return ""
	}

	var inner openAIMessage
	if err := json.Unmarshal(data, &inner); err != nil {
		return ""
	}

	// content might be a plain string.
	var plainContent string
	if err := json.Unmarshal(inner.Content, &plainContent); err == nil {
		return plainContent
	}

	// content is an array of content blocks.
	var blocks []openAIContentBlock
	if err := json.Unmarshal(inner.Content, &blocks); err != nil {
		return ""
	}

	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case outputText:
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		case "tool_use":
			name := b.Name
			if name == "" {
				name = unknownValue
			}
			label := fmt.Sprintf("[tool_use: %s]", name)
			if b.Text != "" {
				label += " " + b.Text
			}
			parts = append(parts, label)
		case "tool_result":
			if b.Text != "" {
				t := b.Text
				if len(t) > 500 {
					t = t[:500] + "..."
				}
				parts = append(parts, t)
			}
		}
	}

	return strings.Join(parts, "\n")
}
