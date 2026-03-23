package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/mcpclient"
)

var (
	runPersonality       string
	runToolchain         string
	runPlugins           []string
	runPort              int
	runEnv               []string
	runEnvForward        []string
	runSecretEnv         []string
	runSecretFile        []string
	runMcpServer         []string
	runPermMode          string
	runModel             string
	runSystemPrompt      string
	runMaxBudget         float64
	runSource            string
	runPersistentMode    bool
	runNoIsolate         bool
	runNoFetch           bool
	runGitAuthor         string
	runGitCredHelper     string
	runGitHTTPSInsteadOf bool
	runYes               bool
	runForce             bool
	runGenerateSuffix    bool

	runMessage  string
	runBlocking bool
	runOutput   string
)

var runCmd = &cobra.Command{
	Use:   "run <name> [workspace]",
	Short: "Create an instance and send a prompt in one step",
	Long: `Create a new klaus instance, wait for it to become ready, and send a
prompt — all in a single command.

This combines 'klausctl create' and 'klausctl prompt' into one operation,
reducing boilerplate in agent workflows. All flags from 'create' are
supported, plus -m/--message to supply the prompt.

By default the command returns once the prompt is accepted. Use --blocking
to wait for the agent to finish and print the result.

Examples:

  klausctl run dev ~/myproject -m "Fix the failing tests"
  klausctl run dev ~/myproject --personality go -m "List all TODO comments" --blocking
  klausctl run dev -m "Refactor the handler" --blocking -o json`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runRun,
}

func init() {
	runCmd.Flags().StringVar(&runPersonality, "personality", "", "personality short name or OCI reference")
	runCmd.Flags().StringVar(&runToolchain, "toolchain", "", "toolchain short name or OCI reference")
	runCmd.Flags().StringSliceVar(&runPlugins, "plugin", nil, "additional plugin short name or OCI reference (repeatable)")
	runCmd.Flags().IntVar(&runPort, "port", 0, "override auto-selected port")
	runCmd.Flags().StringArrayVar(&runEnv, "env", nil, "environment variable KEY=VALUE (repeatable)")
	runCmd.Flags().StringArrayVar(&runEnvForward, "env-forward", nil, "host environment variable name to forward (repeatable)")
	runCmd.Flags().StringVar(&runPermMode, "permission-mode", "", "Claude permission mode: default, acceptEdits, bypassPermissions, dontAsk, plan, delegate")
	runCmd.Flags().StringVar(&runModel, "model", "", "Claude model (e.g. sonnet, opus)")
	runCmd.Flags().StringVar(&runSystemPrompt, "system-prompt", "", "system prompt override for the Claude agent")
	runCmd.Flags().Float64Var(&runMaxBudget, "max-budget", 0, "maximum dollar budget per invocation (0 = no limit)")
	runCmd.Flags().StringArrayVar(&runSecretEnv, "secret-env", nil, "secret env var ENV_NAME=secret-name (repeatable)")
	runCmd.Flags().StringArrayVar(&runSecretFile, "secret-file", nil, "secret file /container/path=secret-name (repeatable)")
	runCmd.Flags().StringArrayVar(&runMcpServer, "mcpserver", nil, "managed MCP server name (repeatable)")
	runCmd.Flags().StringVar(&runSource, "source", "", "resolve artifact short names against a specific source")
	runCmd.Flags().BoolVar(&runPersistentMode, "persistent-mode", false, "enable bidirectional stream-json mode (automatically disables noSessionPersistence)")
	runCmd.Flags().BoolVar(&runNoIsolate, "no-isolate", false, "skip git worktree creation and bind-mount workspace directly")
	runCmd.Flags().BoolVar(&runNoFetch, "no-fetch", false, "skip git fetch origin before cloning the workspace")
	runCmd.Flags().StringVar(&runGitAuthor, "git-author", "", `git author identity "Name <email>"`)
	runCmd.Flags().StringVar(&runGitCredHelper, "git-credential-helper", "", "git credential helper (currently only 'gh')")
	runCmd.Flags().BoolVar(&runGitHTTPSInsteadOf, "git-https-instead-of-ssh", false, "rewrite SSH git URLs to HTTPS via container-local gitconfig")
	runCmd.Flags().BoolVarP(&runYes, "yes", "y", false, "auto-confirm replacement of existing instances")
	runCmd.Flags().BoolVar(&runForce, "force", false, "allow replacing a running instance (prompts for confirmation unless -y is also set)")
	runCmd.Flags().BoolVar(&runGenerateSuffix, "generate-suffix", true, "append a random 4-character suffix to the instance name (use --no-generate-suffix to disable)")

	runCmd.Flags().StringVarP(&runMessage, "message", "m", "", "prompt message to send to the agent (required)")
	runCmd.Flags().BoolVar(&runBlocking, "blocking", false, "wait for the agent to complete and return the result")
	runCmd.Flags().StringVarP(&runOutput, "output", "o", "text", "output format: text, json")
	_ = runCmd.MarkFlagRequired("message")
	rootCmd.AddCommand(runCmd)
}

func runRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	workspace := ""
	if len(args) > 1 {
		workspace = args[1]
	}

	params := CLICreateParams{
		BaseName:        args[0],
		Workspace:       workspace,
		Personality:     runPersonality,
		Toolchain:       runToolchain,
		Plugins:         runPlugins,
		Port:            runPort,
		Env:             runEnv,
		EnvForward:      runEnvForward,
		SecretEnv:       runSecretEnv,
		SecretFile:      runSecretFile,
		McpServer:       runMcpServer,
		PermMode:        runPermMode,
		Model:           runModel,
		SystemPrompt:    runSystemPrompt,
		MaxBudget:       runMaxBudget,
		MaxBudgetSet:    cmd.Flags().Changed("max-budget"),
		Source:          runSource,
		PersistentMode:  runPersistentMode,
		NoIsolate:       runNoIsolate,
		NoFetch:         runNoFetch,
		GitAuthor:       runGitAuthor,
		GitCredHelper:   runGitCredHelper,
		GitHTTPSInstead: runGitHTTPSInsteadOf,
		Yes:             runYes,
		Force:           runForce,
		GenerateSuffix:  runGenerateSuffix,
	}

	instanceName, err := cliCreateInstance(ctx, cmd, params)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()

	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	instancePaths := paths.ForInstance(instanceName)

	// Load instance state to get the port for MCP communication.
	inst, err := instance.Load(instancePaths)
	if err != nil {
		return fmt.Errorf("loading instance state after create: %w", err)
	}

	baseURL := fmt.Sprintf("http://localhost:%d/mcp", inst.Port)

	client := mcpclient.New(buildVersion)
	defer client.Close()

	// Wait for the MCP endpoint inside the container to become ready.
	if err := waitForMCPReady(ctx, instanceName, baseURL, client); err != nil {
		return fmt.Errorf("waiting for instance %q to become ready: %w", instanceName, err)
	}

	fmt.Fprintf(out, "\nSending prompt to %s...\n", instanceName)

	toolResult, err := client.Prompt(ctx, instanceName, baseURL, runMessage)
	if err != nil {
		return fmt.Errorf("sending prompt to %q: %w", instanceName, err)
	}

	if !runBlocking {
		result := promptCLIResult{
			Instance:  instanceName,
			Status:    "started",
			SessionID: client.SessionID(instanceName),
			Result:    extractMCPText(toolResult),
		}
		return renderRunResult(out, result)
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
	return renderRunResult(out, result)
}

func renderRunResult(out io.Writer, result promptCLIResult) error {
	if runOutput == "json" {
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

// waitForMCPReady polls the agent's MCP endpoint until it responds
// successfully or the context is cancelled. This handles the delay between
// container start and the MCP server being fully initialized.
func waitForMCPReady(ctx context.Context, instanceName, baseURL string, client *mcpclient.Client) error {
	poll := 2 * time.Second
	const maxPoll = 10 * time.Second
	const maxWait = 2 * time.Minute

	deadline := time.Now().Add(maxWait)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for MCP endpoint", maxWait)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}

		_, err := client.Status(ctx, instanceName, baseURL)
		if err == nil {
			return nil
		}

		if poll < maxPoll {
			poll = min(poll*2, maxPoll)
		}
	}
}
